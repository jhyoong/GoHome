package guard

// SetYolo enables or disables yolo mode. When yolo is true, Check() allows
// every tool call without consulting the whitelist or frontend.
// Safe for concurrent use (backed by sync/atomic.Bool).
func (g *Guard) SetYolo(v bool) {
	g.yolo.Store(v)
}

// Yolo reports the current yolo mode state.
// Safe for concurrent use.
func (g *Guard) Yolo() bool {
	return g.yolo.Load()
}
