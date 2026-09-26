package provider

// AgentSession is what herdr reports about the agent session a pane is
// displaying: a label and a value, and nothing else this plugin reads.
//
// herdr's report also names the integration that sent it and the agent it is
// for. Neither is decoded: the pane's own agent label has already chosen the
// provider, and neither field changes how the value is read, so they are left
// for the decoder to drop rather than carried unread.
type AgentSession struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
