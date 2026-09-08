package pkg_timeout

import (
	"testing"
	"time"
)

func TestHangsForever(t *testing.T) { time.Sleep(10 * time.Minute) }
