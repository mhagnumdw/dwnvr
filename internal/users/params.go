package users

import "time"

// The numbers of user management. Changing one of them is changing the line
// here.
const (
	// InviteValidity is how long the invite link is valid after it's
	// generated. Short on purpose: the idea is to generate the link while
	// talking to the person, who opens it right away. Once expired, the admin
	// generates another.
	InviteValidity = 10 * time.Minute

	// TokenBytes is the size of the link token, from crypto/rand: 256 bits
	// can't be guessed, so the link needs no attempt limit.
	TokenBytes = 32

	// MinPasswordLen and MaxPasswordLen are the password limits, in
	// characters, and there's no composition rule: it's what NIST SP 800-63B
	// asks for. The ceiling only exists so the request body has a known size;
	// NIST asks to accept at least 64.
	MinPasswordLen = 8
	MaxPasswordLen = 128

	// MaxNameLen is the ceiling of the display name, in characters.
	MaxNameLen = 60

	// PBKDF2Iterations is the cost of each password: 600,000 is OWASP's number
	// for PBKDF2-HMAC-SHA256, and gives 0.77 s per login on an Orange Pi Zero
	// 3 (measured on 10/07/2026, see docs/TODO/TODO_hash-da-senha.md). The
	// iterations go in the stored value: raising this number later doesn't
	// invalidate existing passwords, which keep the number from when they
	// were set.
	PBKDF2Iterations = 600_000

	// MinIterations and MaxIterations are the range accepted when reading a
	// stored value. Below it, the password would be too weakly protected;
	// above it, a corrupted value in the file would tie up a core for minutes
	// on every login.
	MinIterations = 100_000
	MaxIterations = 10_000_000

	// SaltBytes is the salt size, from crypto/rand, one per password.
	SaltBytes = 16

	// HashBytes is the size of the stored hash: SHA-256's.
	HashBytes = 32

	// MaxUsernameLen is the ceiling of the login name.
	MaxUsernameLen = 32

	// MaxEmailLen is the ceiling of the person's e-mail, that of an address
	// per RFC 5321.
	MaxEmailLen = 254

	// AvatarSide is the side, in pixels, of the square the browser crops and
	// scales the chosen photo to. 256 keeps the largest avatar drawing on
	// screen (88 px) sharp on a density-3 phone.
	AvatarSide = 256

	// MaxAvatarBytes is the largest JPEG accepted, in bytes, already scaled
	// down. At AvatarQuality, the photos measured on 10/09/2026 came out at 10
	// to 23 KB; pure noise, the worst case, gave 29 KB at quality 80.
	MaxAvatarBytes = 32 << 10

	// AvatarQuality is the quality of the JPEG the browser generates, from 0
	// to 100. It only lowers the quality if the result goes over
	// MaxAvatarBytes.
	AvatarQuality = 92

	// AvatarIDBytes is the size of the avatar file name, from crypto/rand.
	// The name changes on every swap and goes in the URL, which lets the
	// browser cache the image forever.
	AvatarIDBytes = 8

	// LastAccessStep is how often a person's last access moves forward. The
	// screen shows it down to the minute; with a smaller step, the requests of
	// someone using it would touch the map for nothing.
	LastAccessStep = time.Minute

	// AccessFlushDelay is how long a new last access waits in memory before
	// going to acessos.json. It's the most that's lost in a power cut; on a
	// normal shutdown, the pending one is written right away.
	AccessFlushDelay = 5 * time.Minute
)
