package mgi

type MGIService interface {
	Add(files []string) error
	Commit(message string) error
	Diff() (string, error)
	Show() (string, error)
	Status() (string, error)
	Pull(remote string) error
	Push(remote string) error
}
