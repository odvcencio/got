package repo

import (
	"math"
	"os"
	"reflect"

	"github.com/odvcencio/got/pkg/object"
)

type statusFileFingerprint struct {
	Mode           string
	ModTimeNano    int64
	Size           int64
	HasChangeTime  bool
	ChangeTimeNano int64
	HasFileID      bool
	Device         uint64
	Inode          uint64
}

type statusFileHashCacheEntry struct {
	Fingerprint statusFileFingerprint
	BlobHash    object.Hash
}

func (r *Repo) invalidateStatusCache() {
	r.statusHashCacheMu.Lock()
	r.statusHashCache = nil
	r.statusHashCacheMu.Unlock()
}

func (r *Repo) worktreeBlobHash(path, absPath string, info os.FileInfo, mode string) (object.Hash, error) {
	fingerprint := statusFingerprintFromFileInfo(info, mode)
	if blobHash, ok := r.statusHashCacheLookup(path, fingerprint); ok {
		return blobHash, nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}

	blobHash := r.statusBlobHash(data)
	r.statusHashCacheStore(path, fingerprint, blobHash)
	return blobHash, nil
}

func (r *Repo) statusHashCacheLookup(path string, fingerprint statusFileFingerprint) (object.Hash, bool) {
	r.statusHashCacheMu.Lock()
	defer r.statusHashCacheMu.Unlock()

	entry, ok := r.statusHashCache[path]
	if !ok {
		return "", false
	}
	if entry.Fingerprint != fingerprint {
		return "", false
	}
	return entry.BlobHash, true
}

func (r *Repo) statusHashCacheStore(path string, fingerprint statusFileFingerprint, blobHash object.Hash) {
	r.statusHashCacheMu.Lock()
	defer r.statusHashCacheMu.Unlock()

	if r.statusHashCache == nil {
		r.statusHashCache = make(map[string]statusFileHashCacheEntry)
	}
	r.statusHashCache[path] = statusFileHashCacheEntry{
		Fingerprint: fingerprint,
		BlobHash:    blobHash,
	}
}

func (r *Repo) statusBlobHash(data []byte) object.Hash {
	if r.statusBlobHasher != nil {
		return r.statusBlobHasher(data)
	}
	return object.HashObject(object.TypeBlob, data)
}

func statusFingerprintFromFileInfo(info os.FileInfo, mode string) statusFileFingerprint {
	fingerprint := statusFileFingerprint{
		Mode:        normalizeFileMode(mode),
		ModTimeNano: info.ModTime().UnixNano(),
		Size:        info.Size(),
	}

	if changeTimeNano, ok := statusChangeTimeUnixNano(info); ok {
		fingerprint.HasChangeTime = true
		fingerprint.ChangeTimeNano = changeTimeNano
	}

	if dev, ino, ok := statusDeviceAndInode(info); ok {
		fingerprint.HasFileID = true
		fingerprint.Device = dev
		fingerprint.Inode = ino
	}

	return fingerprint
}

func statusDeviceAndInode(info os.FileInfo) (uint64, uint64, bool) {
	statValue, ok := statusStatStruct(info)
	if !ok {
		return 0, 0, false
	}

	dev, ok := statusUintFieldByNames(statValue, "Dev")
	if !ok {
		return 0, 0, false
	}
	ino, ok := statusUintFieldByNames(statValue, "Ino")
	if !ok {
		return 0, 0, false
	}
	return dev, ino, true
}

func statusChangeTimeUnixNano(info os.FileInfo) (int64, bool) {
	statValue, ok := statusStatStruct(info)
	if !ok {
		return 0, false
	}

	for _, name := range []string{"Ctim", "Ctimespec"} {
		if tsField := statValue.FieldByName(name); tsField.IsValid() {
			if nano, ok := statusTimespecUnixNano(tsField); ok {
				return nano, true
			}
		}
	}

	sec, hasSec := statusIntFieldByNames(statValue, "Ctime")
	nsec, hasNsec := statusIntFieldByNames(statValue, "CtimeNsec", "Ctimensec")
	if hasSec && hasNsec {
		return sec*1_000_000_000 + nsec, true
	}

	return 0, false
}

func statusTimespecUnixNano(v reflect.Value) (int64, bool) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}

	sec, hasSec := statusIntFieldByNames(v, "Sec", "Tv_sec")
	nsec, hasNsec := statusIntFieldByNames(v, "Nsec", "Tv_nsec")
	if !hasSec || !hasNsec {
		return 0, false
	}
	return sec*1_000_000_000 + nsec, true
}

func statusStatStruct(info os.FileInfo) (reflect.Value, bool) {
	sys := info.Sys()
	if sys == nil {
		return reflect.Value{}, false
	}

	v := reflect.ValueOf(sys)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return v, true
}

func statusUintFieldByNames(v reflect.Value, names ...string) (uint64, bool) {
	for _, name := range names {
		f := v.FieldByName(name)
		if !f.IsValid() {
			continue
		}
		if u, ok := statusUint64Value(f); ok {
			return u, true
		}
	}
	return 0, false
}

func statusIntFieldByNames(v reflect.Value, names ...string) (int64, bool) {
	for _, name := range names {
		f := v.FieldByName(name)
		if !f.IsValid() {
			continue
		}
		if i, ok := statusInt64Value(f); ok {
			return i, true
		}
	}
	return 0, false
}

func statusUint64Value(v reflect.Value) (uint64, bool) {
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := v.Int()
		if i < 0 {
			return 0, false
		}
		return uint64(i), true
	default:
		return 0, false
	}
}

func statusInt64Value(v reflect.Value) (int64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := v.Uint()
		if u > math.MaxInt64 {
			return 0, false
		}
		return int64(u), true
	default:
		return 0, false
	}
}
