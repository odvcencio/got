package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
)

const (
	pushChunkObjectLimit = 2000
	pushChunkByteLimit   = 32 << 20
	pushObjectByteLimit  = 16 << 20
)

type pushLimitObject struct {
	Hash      object.Hash
	Type      object.ObjectType
	SizeBytes int64
}

type pushLimitReport struct {
	PushTarget      string
	RemoteName      string
	RemoteURL       string
	LocalRef        string
	RemoteRef       string
	LocalHash       object.Hash
	RemoteHash      object.Hash
	LimitBytes      int64
	ObjectsExamined int
	TotalBytes      int64
	Largest         *pushLimitObject
	Blockers        []pushLimitObject
}

func collectPushLimitReport(ctx context.Context, r *repo.Repo, pushTarget, localRef, remoteName, remoteURL, remoteRef string) (*pushLimitReport, error) {
	localHash, err := r.ResolveRef(localRef)
	if err != nil {
		return nil, fmt.Errorf("resolve local ref %q: %w", localRef, err)
	}

	report := &pushLimitReport{
		PushTarget: pushTarget,
		RemoteName: remoteName,
		RemoteURL:  remoteURL,
		LocalRef:   localRef,
		RemoteRef:  remoteRef,
		LocalHash:  localHash,
		LimitBytes: pushObjectByteLimit,
	}

	var stopRoots []object.Hash
	if strings.TrimSpace(remoteURL) != "" {
		client, err := remote.NewClient(remoteURL)
		if err != nil {
			return nil, err
		}
		remoteRefs, err := client.ListRefs(ctx)
		if err != nil {
			return nil, err
		}
		if remoteRef != "" {
			report.RemoteHash = remoteRefs[remoteRef]
		}
		stopRoots = make([]object.Hash, 0, len(remoteRefs))
		for _, h := range remoteRefs {
			if strings.TrimSpace(string(h)) == "" {
				continue
			}
			if r.Store.Has(h) {
				stopRoots = append(stopRoots, h)
			}
		}
	}

	objectsToPush, err := remote.CollectObjectsForPush(r.Store, []object.Hash{localHash}, stopRoots)
	if err != nil {
		return nil, err
	}

	report.ObjectsExamined = len(objectsToPush)
	for _, obj := range objectsToPush {
		size := int64(len(obj.Data))
		report.TotalBytes += size

		current := pushLimitObject{
			Hash:      obj.Hash,
			Type:      obj.Type,
			SizeBytes: size,
		}
		if report.Largest == nil || current.SizeBytes > report.Largest.SizeBytes {
			largest := current
			report.Largest = &largest
		}
		if size > report.LimitBytes {
			report.Blockers = append(report.Blockers, current)
		}
	}

	sort.Slice(report.Blockers, func(i, j int) bool {
		if report.Blockers[i].SizeBytes == report.Blockers[j].SizeBytes {
			return report.Blockers[i].Hash < report.Blockers[j].Hash
		}
		return report.Blockers[i].SizeBytes > report.Blockers[j].SizeBytes
	})

	return report, nil
}

func printPushLimitSummary(w io.Writer, report *pushLimitReport) {
	if report == nil {
		return
	}
	if report.ObjectsExamined == 0 {
		if strings.TrimSpace(report.RemoteRef) != "" {
			fmt.Fprintf(w, "ok: no objects need to be pushed for %s\n", report.PushTarget)
		} else {
			fmt.Fprintln(w, "ok: no objects exceed the push limit")
		}
		return
	}

	largestSummary := "none"
	if report.Largest != nil {
		largestSummary = fmt.Sprintf("%s %s (%s)", formatBinaryBytes(report.Largest.SizeBytes), report.Largest.Type, shortHash(report.Largest.Hash))
	}
	fmt.Fprintf(
		w,
		"ok: checked %d object(s), largest %s, no object exceeds %s\n",
		report.ObjectsExamined,
		largestSummary,
		formatBinaryBytes(report.LimitBytes),
	)
}

func pushLimitError(report *pushLimitReport) error {
	if report == nil || len(report.Blockers) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(
		&b,
		"push limit check failed: %d object(s) exceed the %s object limit",
		len(report.Blockers),
		formatBinaryBytes(report.LimitBytes),
	)
	if report.RemoteName != "" {
		fmt.Fprintf(&b, " for remote %q", report.RemoteName)
	}
	b.WriteByte('\n')
	for _, blocker := range report.Blockers {
		fmt.Fprintf(
			&b,
			"  %s  %s  %s\n",
			formatBinaryBytes(blocker.SizeBytes),
			blocker.Type,
			shortHash(blocker.Hash),
		)
	}
	if report.Largest != nil && len(report.Blockers) == 0 {
		fmt.Fprintf(
			&b,
			"largest object: %s %s (%s)\n",
			formatBinaryBytes(report.Largest.SizeBytes),
			report.Largest.Type,
			shortHash(report.Largest.Hash),
		)
	}

	return errors.New(strings.TrimRight(b.String(), "\n"))
}

func jsonVerifyPushLimitReport(report *pushLimitReport) JSONVerifyPushLimitsOutput {
	result := JSONVerifyPushLimitsOutput{
		OK:              len(report.Blockers) == 0,
		PushTarget:      report.PushTarget,
		Remote:          report.RemoteName,
		LocalRef:        report.LocalRef,
		RemoteRef:       report.RemoteRef,
		LocalHash:       string(report.LocalHash),
		RemoteHash:      string(report.RemoteHash),
		LimitBytes:      report.LimitBytes,
		ObjectsExamined: report.ObjectsExamined,
		TotalBytes:      report.TotalBytes,
	}
	if report.Largest != nil {
		largest := jsonVerifySizedObject(*report.Largest)
		result.Largest = &largest
	}
	if len(report.Blockers) > 0 {
		result.Blockers = make([]JSONVerifySizedObject, 0, len(report.Blockers))
		for _, blocker := range report.Blockers {
			result.Blockers = append(result.Blockers, jsonVerifySizedObject(blocker))
		}
	}
	return result
}

func jsonVerifySizedObject(obj pushLimitObject) JSONVerifySizedObject {
	return JSONVerifySizedObject{
		Hash:      string(obj.Hash),
		ShortHash: shortHash(obj.Hash),
		Type:      string(obj.Type),
		SizeBytes: obj.SizeBytes,
	}
}

func formatBinaryBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div := int64(unit)
	exp := 0
	for n := size / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	value := float64(size) / float64(div)
	suffixes := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	return fmt.Sprintf("%.1f %s", value, suffixes[exp])
}
