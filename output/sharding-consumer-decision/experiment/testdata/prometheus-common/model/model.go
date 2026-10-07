package model

// Only the key representation is simplified. The ShardingConsumer is copied
// unchanged from Alloy main; these tests isolate its scheduling behavior.
type Fingerprint uint64
type LabelSet uint64

func (l LabelSet) FastFingerprint() Fingerprint { return Fingerprint(l) }
