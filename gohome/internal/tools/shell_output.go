package tools

import (
	"fmt"
	"unicode/utf8"
)

// shellOutputKeep is how many bytes of shell output are kept from the start
// and from the end. Output in between is dropped and replaced by a marker.
const shellOutputKeep = 32 * 1024

// cappedBuffer keeps the first headMax bytes and the last tailMax bytes
// written to it, and counts the total. Memory use stays bounded no matter
// how much is written. It is not safe for concurrent use.
type cappedBuffer struct {
	headMax, tailMax int
	head             []byte
	tail             []byte
	total            int64
}

func newCappedBuffer(headMax, tailMax int) *cappedBuffer {
	return &cappedBuffer{headMax: headMax, tailMax: tailMax}
}

// Write never fails.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.total += int64(n)
	if room := b.headMax - len(b.head); room > 0 {
		k := min(room, len(p))
		b.head = append(b.head, p[:k]...)
		p = p[k:]
	}
	if len(p) == 0 {
		return n, nil
	}
	b.tail = append(b.tail, p...)
	// Compact once the tail holds twice its limit, so appends stay cheap.
	if len(b.tail) > 2*b.tailMax {
		b.tail = append(b.tail[:0], b.tail[len(b.tail)-b.tailMax:]...)
	}
	return n, nil
}

// String returns the kept output. If bytes were dropped, a line like
// "[... N bytes omitted ...]" sits between the head and the tail. The cut
// points move by up to 3 bytes so a multi-byte UTF-8 rune is not split;
// moved bytes are counted as omitted.
func (b *cappedBuffer) String() string {
	if b.total <= int64(b.headMax+b.tailMax) {
		return string(b.head) + string(b.tail)
	}
	head := b.head
	tail := b.tail[len(b.tail)-b.tailMax:]

	// Drop an incomplete rune at the end of head.
	for i := 1; i <= utf8.UTFMax-1 && i <= len(head); i++ {
		c := head[len(head)-i]
		if utf8.RuneStart(c) {
			if !utf8.FullRune(head[len(head)-i:]) {
				head = head[:len(head)-i]
			}
			break
		}
	}
	// Drop continuation bytes at the start of tail.
	for i := 0; i < utf8.UTFMax-1 && len(tail) > 0 && !utf8.RuneStart(tail[0]); i++ {
		tail = tail[1:]
	}

	omitted := b.total - int64(len(head)) - int64(len(tail))
	return fmt.Sprintf("%s\n[... %d bytes omitted ...]\n%s", head, omitted, tail)
}
