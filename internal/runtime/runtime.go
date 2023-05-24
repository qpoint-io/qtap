package runtime

// js/ts runtime
type Runtime interface {
	Start(bundle string, listen string) (Stoppable, error)
}

// a stoppable process
type Stoppable interface {
	Stop() error
}

func Factory(engine string) Runtime {
	switch engine {
	case "deno":
		return &Deno{}
	default:
		return nil
	}
}
