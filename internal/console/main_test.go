package console

import (
	"github.com/benpsk/go-starter/internal/testutil"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testutil.Run(m)) }
