package compositor

import "context"

// None is the backend used when no compositor is detected: the shell's
// theme and settings still work, window actions fail cleanly.
type None struct{}

func (None) Name() string                              { return "none" }
func (None) Version(context.Context) string            { return "" }
func (None) Caps() Caps                                { return Caps{} }
func (None) Windows(context.Context) ([]Window, error) { return nil, nil }
func (None) Workspaces(context.Context) ([]Workspace, error) {
	return nil, nil
}
func (None) Outputs(context.Context) ([]Output, error)                { return nil, nil }
func (None) Focus(context.Context, string) error                      { return ErrNoCompositor }
func (None) Close(context.Context, string) error                      { return ErrNoCompositor }
func (None) SetFloating(context.Context, string, bool) error          { return ErrNoCompositor }
func (None) MoveResize(context.Context, string, Rect) error           { return ErrNoCompositor }
func (None) MoveToWorkspace(context.Context, string, Workspace) error { return ErrNoCompositor }
func (None) SwitchWorkspace(context.Context, Workspace) error         { return ErrNoCompositor }
func (None) Spawn(context.Context, []string) error                    { return ErrNoCompositor }
func (None) ApplyStyle(context.Context, Style) error                  { return nil }
func (None) Subscribe(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event)
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}
