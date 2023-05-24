package event

// Sending events to control loops needs to block the caller until
// the event handler has completed. To do this we need to create
// a stateful event with an ad-hoc channel to receive the response
type Op struct {
	Action string     // the requested action (stop/resume/etc)
	Res    chan error // the response channel to syncronize state
}
