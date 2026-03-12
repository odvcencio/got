package gotreesitter

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	parseNodeLimitScaleOnce sync.Once
	parseNodeLimitScale     int
	parseMaxGLRStacksOnce   sync.Once
	parseMaxGLRStacks       int
)

// ResetParseEnvConfigCacheForTests clears memoized parser env config.
//
// Tests in this repo mutate env vars between cases; this helper ensures
// subsequent parses observe the new values in the same process.
func ResetParseEnvConfigCacheForTests() {
	parseNodeLimitScaleOnce = sync.Once{}
	parseNodeLimitScale = 0
	parseMaxGLRStacksOnce = sync.Once{}
	parseMaxGLRStacks = 0
}

func parseNodeLimitScaleFactor() int {
	parseNodeLimitScaleOnce.Do(func() {
		parseNodeLimitScale = 1
		raw := strings.TrimSpace(os.Getenv("GOT_PARSE_NODE_LIMIT_SCALE"))
		if raw == "" {
			return
		}
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 {
			parseNodeLimitScale = n
		}
	})
	return parseNodeLimitScale
}

func parseMaxGLRStacksValue() int {
	parseMaxGLRStacksOnce.Do(func() {
		parseMaxGLRStacks = maxGLRStacks
		raw := strings.TrimSpace(os.Getenv("GOT_GLR_MAX_STACKS"))
		if raw == "" {
			return
		}
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 {
			parseMaxGLRStacks = n
		}
	})
	return parseMaxGLRStacks
}
