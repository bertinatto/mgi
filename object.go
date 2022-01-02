package mgi

const (
	BlobType   = "blob"
	TreeType   = "tree"
	CommitType = "commit"
)

type ObjectService interface {
	Store(objType string, data []byte) (hash string, sha1 [20]byte, err error)
	Read(hash string) (objType string, data []byte, err error)
}
