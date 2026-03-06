package config

import "time"

const (
	DefaultMaxContextTokens  = 128000
	DefaultMaxOutputTokens   = 4096
	DefaultMaxToolIterations = 25

	LLMMaxRetries     = 3
	LLMBaseRetryDelay = 5 * time.Second
	LLMMaxRetryDelay  = 60 * time.Second
	LLMMaxErrors      = 2

	MaxSubagentDepth = 3

	CompactionThreshold  = 40
	CompactionKeepRecent = 10
	CompactionMaxTokens  = 1024

	ImageTokenEstimate     = 1600
	MaxRecentImageMessages = 3
	RecentMessagesLimit    = 50

	TurnEventBufferSize = 64

	ErrorTruncateLen      = 300
	ToolResultTruncateLen = 200

	DefaultRetryMaxAttempts = 3
	DefaultRetryCooldownMS  = 500

	DefaultCheckpointTokenThreshold = 10000
	DefaultCheckpointRetentionHours = 24

	DefaultToolConcurrency = 4
	DefaultToolTimeout     = 2 * time.Minute
	DefaultRecoveryRetries = 2
	RecoveryBaseDelay      = 1 * time.Second
	RecoveryMaxDelay       = 10 * time.Second
)
