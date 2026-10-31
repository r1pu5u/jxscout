package common

// TailBuffer retains only the last limit bytes of subprocess diagnostics.
// Write always consumes the whole input so subprocess pipes cannot block.
type TailBuffer struct {
	limit int
	data  []byte
}

func NewTailBuffer(limit int) *TailBuffer { return &TailBuffer{limit: max(0, limit)} }
func (b *TailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.limit == 0 {
		return n, nil
	}
	if len(p) >= b.limit {
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
		return n, nil
	}
	if drop := len(b.data) + len(p) - b.limit; drop > 0 {
		copy(b.data, b.data[drop:])
		b.data = b.data[:len(b.data)-drop]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *TailBuffer) String() string { return string(b.data) }
