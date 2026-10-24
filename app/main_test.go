package app

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	SetAddressPrefixes()
	os.Exit(m.Run())
}
