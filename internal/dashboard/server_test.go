package dashboard

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestExtractConfigNameWithFolder(t *testing.T) {
	for _, key := range []string{
		"sw6-sql/db/2026-01-02/030405.tar.zst",
		"sw6/sw6-sql/db/2026-01-02/030405.tar.zst",
	} {
		assert.Equal(t, "db", extractConfigName(key))
	}
}
