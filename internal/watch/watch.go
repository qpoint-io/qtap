package watch

// version watcher
type Watcher interface {
	Watch() (chan string, error)
	Stop() error
}
