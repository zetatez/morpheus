package app

import (
	"strings"
	"testing"
)

func TestStringBuilderPool(t *testing.T) {
	sb := GetStringBuilder()
	defer PutStringBuilder(sb)

	sb.WriteString("hello")
	sb.WriteString(" ")
	sb.WriteString("world")

	if sb.String() != "hello world" {
		t.Errorf("expected 'hello world', got %q", sb.String())
	}
}

func TestStringBuilderPoolReuse(t *testing.T) {
	sb1 := GetStringBuilder()
	sb1.WriteString("first")
	s1 := sb1.String()
	PutStringBuilder(sb1)

	sb2 := GetStringBuilder()
	defer PutStringBuilder(sb2)

	if sb2.Len() != 0 {
		t.Errorf("expected empty builder after reset, got len %d", sb2.Len())
	}

	sb2.WriteString("second")
	if sb2.String() == s1 {
		t.Errorf("expected new content, should not be %q", s1)
	}
}

func TestBufferPool(t *testing.T) {
	buf := GetBuffer()
	defer PutBuffer(buf)

	buf.WriteString("test data")
	if buf.String() != "test data" {
		t.Errorf("expected 'test data', got %q", buf.String())
	}
}

func TestBufferPoolReuse(t *testing.T) {
	buf1 := GetBuffer()
	buf1.WriteString("content1")
	PutBuffer(buf1)

	buf2 := GetBuffer()
	defer PutBuffer(buf2)

	if buf2.Len() != 0 {
		t.Errorf("expected empty buffer after reset, got len %d", buf2.Len())
	}
}

func TestPoolStress(t *testing.T) {
	for i := 0; i < 100; i++ {
		sb := GetStringBuilder()
		for j := 0; j < 100; j++ {
			sb.WriteString("x")
		}
		result := sb.String()
		if len(result) != 100 {
			t.Errorf("expected 100 chars, got %d", len(result))
		}
		PutStringBuilder(sb)

		buf := GetBuffer()
		for j := 0; j < 100; j++ {
			buf.WriteByte(byte('y'))
		}
		if buf.Len() != 100 {
			t.Errorf("expected 100 bytes, got %d", buf.Len())
		}
		PutBuffer(buf)
	}
}

func TestStringBuilderConcurrent(t *testing.T) {
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				sb := GetStringBuilder()
				sb.WriteString(strings.Repeat("a", 100))
				PutStringBuilder(sb)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
