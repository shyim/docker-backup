package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestNewHtpasswdAuth_InlineCredentials(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	require.NoError(t, err)

	input := "admin:" + string(hash)
	auth, err := NewHtpasswdAuth(input)
	require.NoError(t, err)
	assert.Equal(t, 1, auth.UserCount())
}

func TestNewHtpasswdAuth_MultipleInlineCredentials(t *testing.T) {
	hash1, _ := bcrypt.GenerateFromPassword([]byte("pass1"), bcrypt.MinCost)
	hash2, _ := bcrypt.GenerateFromPassword([]byte("pass2"), bcrypt.MinCost)

	input := "user1:" + string(hash1) + "\nuser2:" + string(hash2)
	auth, err := NewHtpasswdAuth(input)
	require.NoError(t, err)
	assert.Equal(t, 2, auth.UserCount())
}

func TestNewHtpasswdAuth_FromFile(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)

	tmpDir := t.TempDir()
	htpasswdFile := filepath.Join(tmpDir, "htpasswd")
	content := "testuser:" + string(hash) + "\n"
	require.NoError(t, os.WriteFile(htpasswdFile, []byte(content), 0600))

	auth, err := NewHtpasswdAuth(htpasswdFile)
	require.NoError(t, err)
	assert.Equal(t, 1, auth.UserCount())
	assert.True(t, auth.Authenticate("testuser", "testpass"))
}

func TestNewHtpasswdAuth_FileWithComments(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)

	tmpDir := t.TempDir()
	htpasswdFile := filepath.Join(tmpDir, "htpasswd")
	content := "# This is a comment\n\nuser1:" + string(hash) + "\n# Another comment\n"
	require.NoError(t, os.WriteFile(htpasswdFile, []byte(content), 0600))

	auth, err := NewHtpasswdAuth(htpasswdFile)
	require.NoError(t, err)
	assert.Equal(t, 1, auth.UserCount())
}

func TestNewHtpasswdAuth_EmptyInput(t *testing.T) {
	_, err := NewHtpasswdAuth("")
	assert.Error(t, err)
}

func TestNewHtpasswdAuth_InvalidFormat(t *testing.T) {
	_, err := NewHtpasswdAuth("invalidline")
	assert.Error(t, err)
}

func TestNewHtpasswdAuth_EmptyUsername(t *testing.T) {
	_, err := NewHtpasswdAuth(":somehash")
	assert.Error(t, err)
}

func TestNewHtpasswdAuth_EmptyHash(t *testing.T) {
	_, err := NewHtpasswdAuth("user:")
	assert.Error(t, err)
}

func TestAuthenticate_BcryptValid(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.MinCost)
	auth, _ := NewHtpasswdAuth("admin:" + string(hash))

	assert.True(t, auth.Authenticate("admin", "correctpassword"))
}

func TestAuthenticate_BcryptInvalid(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.MinCost)
	auth, _ := NewHtpasswdAuth("admin:" + string(hash))

	assert.False(t, auth.Authenticate("admin", "wrongpassword"))
}

func TestAuthenticate_UnknownUser(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	auth, _ := NewHtpasswdAuth("admin:" + string(hash))

	assert.False(t, auth.Authenticate("unknown", "password"))
}

func TestAuthenticate_SHA1Valid(t *testing.T) {
	// SHA1 hash of "password" in htpasswd format: {SHA}W6ph5Mm5Pz8GgiULbPgzG37mj9g=
	auth, err := NewHtpasswdAuth("user:{SHA}W6ph5Mm5Pz8GgiULbPgzG37mj9g=")
	require.NoError(t, err)

	assert.True(t, auth.Authenticate("user", "password"))
}

func TestAuthenticate_SHA1Invalid(t *testing.T) {
	auth, _ := NewHtpasswdAuth("user:{SHA}W6ph5Mm5Pz8GgiULbPgzG37mj9g=")
	assert.False(t, auth.Authenticate("user", "wrongpassword"))
}

func TestAuthenticate_PlainText(t *testing.T) {
	auth, _ := NewHtpasswdAuth("user:plaintextpassword")

	assert.True(t, auth.Authenticate("user", "plaintextpassword"))
	assert.False(t, auth.Authenticate("user", "wrongpassword"))
}

func TestAuthenticate_Apr1NotSupported(t *testing.T) {
	auth, _ := NewHtpasswdAuth("user:$apr1$salt$hashedvalue")
	assert.False(t, auth.Authenticate("user", "anypassword"))
}

func TestAuthenticate_Bcrypt2y(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("mypass"), bcrypt.MinCost)
	auth, _ := NewHtpasswdAuth("user:" + string(hash))

	assert.True(t, auth.Authenticate("user", "mypass"))
}

func TestUserCount(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"single user", "user1:" + string(hash), 1},
		{"two users", "user1:" + string(hash) + "\nuser2:" + string(hash), 2},
		{"three users", "user1:" + string(hash) + "\nuser2:" + string(hash) + "\nuser3:" + string(hash), 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := NewHtpasswdAuth(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, auth.UserCount())
		})
	}
}

func TestCheckPassword_InvalidSHA1Base64(t *testing.T) {
	result := checkPassword("password", "{SHA}invalid-base64!!!")
	assert.False(t, result)
}

func TestParseLine_WhitespaceHandling(t *testing.T) {
	auth := &HtpasswdAuth{users: make(map[string]string)}

	err := auth.parseLine("  user  :  hash  ")
	require.NoError(t, err)

	assert.Contains(t, auth.users, "user")
	assert.Equal(t, "hash", auth.users["user"])
}

func TestParseLine_ColonInHash(t *testing.T) {
	auth := &HtpasswdAuth{users: make(map[string]string)}

	err := auth.parseLine("user:hash:with:colons")
	require.NoError(t, err)

	assert.Equal(t, "hash:with:colons", auth.users["user"])
}
