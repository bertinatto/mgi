package mgi

type Index struct {
	Signature  string
	Version    string
	EntryCount int
	Entries    []*IndexEntry
	Hash       string
}

type IndexEntry struct {
	CTimeSecs     uint32
	CTimeNanoSecs uint32
	MTimeSecs     uint32
	MTimeNanoSecs uint32
	Dev           uint32
	Ino           uint32
	Mode          uint32
	Uid           uint32
	Gid           uint32
	FileSize      uint32
	Sha1          [20]byte
	Flags         uint16
	Path          string
}

type IndexService interface {
	Add(path string, sum [20]byte) error
	Read() (*Index, error)
	Store() error
}
