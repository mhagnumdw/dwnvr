package recorder

import "time"

// ReconnectWindow is the "recent" of reconnections: the diagnostics screen
// says how many dropped within it, next to the total since counting started.
// The total alone doesn't tell a link that just got worse from one that had a
// bad night days ago.
const ReconnectWindow = 8 * time.Hour
