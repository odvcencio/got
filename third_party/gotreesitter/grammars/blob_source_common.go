package grammars

type grammarBlob struct {
	data    []byte
	release func()
}

func (b grammarBlob) close() {
	if b.release != nil {
		b.release()
	}
}
