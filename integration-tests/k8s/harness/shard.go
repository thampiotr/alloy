package harness

import (
	"flag"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"testing"
)

var shardFlag = flag.String("shard", "", "run only tests for shard i/n")

type shardConfig struct {
	index int
	total int
}

// ValidateShard returns nil if s parses as a valid "i/n" expression (n > 0
// and 0 <= i < n), or a descriptive error otherwise. The empty string is
// treated as invalid here; callers that want to allow "no sharding" should
// short-circuit before calling.
//
// This is the single source of truth for the i/n grammar, used by the test
// harness's --shard flag and by the runner's interactive form validation.
func ValidateShard(s string) error {
	_, err := parseShardString(s)
	return err
}

func parseShardString(s string) (shardConfig, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return shardConfig{}, fmt.Errorf("invalid shard %q, expected i/n", s)
	}
	index, err := strconv.Atoi(parts[0])
	if err != nil {
		return shardConfig{}, fmt.Errorf("invalid shard index in %q: %w", s, err)
	}
	total, err := strconv.Atoi(parts[1])
	if err != nil {
		return shardConfig{}, fmt.Errorf("invalid shard total in %q: %w", s, err)
	}
	if total <= 0 {
		return shardConfig{}, fmt.Errorf("invalid shard total %d", total)
	}
	if index < 0 || index >= total {
		return shardConfig{}, fmt.Errorf("invalid shard index %d for total %d", index, total)
	}
	return shardConfig{index: index, total: total}, nil
}

func parseShard() (shardConfig, error) {
	if *shardFlag == "" {
		return shardConfig{}, nil
	}
	return parseShardString(*shardFlag)
}

func (s shardConfig) shouldRun(key string) bool {
	if s.total == 0 {
		return true
	}
	// Sharding is done at the test-package level.
	// The package key is hashed so each shard gets a stable subset.
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(key))
	return int(hasher.Sum32()%uint32(s.total)) == s.index
}

func shardCheck(t *testing.T, name string) {
	t.Helper()
	shard, err := parseShard()
	if err != nil {
		t.Fatalf("invalid shard flag: %v", err)
	}
	if shard.total == 0 {
		return
	}
	if !shard.shouldRun(name) {
		t.Skipf("skipping %s for shard %d/%d", name, shard.index, shard.total)
	}
}
