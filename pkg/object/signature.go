package object

// CommitSigningPayload returns the canonical bytes that are signed for a
// commit. The payload intentionally excludes the signature field itself.
func CommitSigningPayload(c *CommitObj) []byte {
	if c == nil {
		return nil
	}
	copyCommit := *c
	copyCommit.Signature = ""
	return MarshalCommit(&copyCommit)
}
