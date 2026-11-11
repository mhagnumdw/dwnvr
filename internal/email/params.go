package email

import "time"

// The numbers for sending e-mail. Changing one of them is changing the line
// here.
const (
	// SendTimeout is the most a send waits, from connecting to the server's
	// answer to the message. The slack covers a slow server; whoever waits is
	// the screen, with the admin looking.
	SendTimeout = 20 * time.Second

	// Base64LineLen is the line length of a base64 attachment: the RFC 2045
	// maximum, which every mail reader expects. Without breaks, a 50 KB frame
	// would become a single line, and SMTP rejects lines longer than 998.
	Base64LineLen = 76
)
