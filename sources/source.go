package sources

type File struct {
	Name        string
	Description string
	Private     bool
}

type Source interface {
	Name() string
	Files() []File
}
