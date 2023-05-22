package control

// version watcher
type watcher interface {
	Get() (string, error)
	Watch() (<-chan string, error)
}

// bundle downloader
type downloader interface {
	Fetch(version string) error
}

// network proxy
type proxy interface {
	Start(from, to string) error
	Replace(to string) error
	Stop() error
}

// js/ts runtime
type runtime interface {
	Start(bundle string, listen string) (string, error)
	Stop(pid string) error
}

type App struct {
	Watcher    watcher
	Downloader downloader
	Proxy      proxy
	Runtime    runtime
}

func (a *App) Run() {
	// get version

	// download bundle

	// start proxy

	// run bundle

	// listen (version updates, stop cmd, etc)
}
