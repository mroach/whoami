package version

import (
	"fmt"
	"strconv"
	"time"
)

var (
	// Git hash of the last commit when this app was built
	CommitHash      string = "none"
	commitTimestamp string = "0"
)

// UNIX timestamp (int) of the last commit before build
var CommitTimestampUnix, _ = strconv.ParseInt(commitTimestamp, 10, 64)

var CommitTimestamp = time.Unix(CommitTimestampUnix, 0)

func ToString() string {
	tsFormatted := CommitTimestamp.Format("2006-01-02T15:04:05")
	return fmt.Sprintf("%s-%s", tsFormatted, CommitHash)
}
