package api

import (
	"testing"
	"time"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecode(t *testing.T) {
	cursor := MessageCursor{
		Timestamp: "2025-10-20T12:00:00Z",
		Index:     42,
	}

	encoded, err := EncodeCursor(cursor)
	require.NoError(t, err)
	assert.NotEmpty(t, encoded)

	decoded, err := DecodeCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, cursor.Timestamp, decoded.Timestamp)
	assert.Equal(t, cursor.Index, decoded.Index)
}

func TestDecodeEmptyCursor(t *testing.T) {
	decoded, err := DecodeCursor("")
	require.NoError(t, err)
	assert.Equal(t, MessageCursor{}, decoded)
}

func TestDecodeInvalidCursor(t *testing.T) {
	_, err := DecodeCursor("not-valid-base64!")
	assert.Error(t, err)
}

func createTestMessages(count int) []session.Message {
	messages := make([]session.Message, count)
	for i := 0; i < count; i++ {
		role := chat.MessageRoleUser
		if i%2 == 1 {
			role = chat.MessageRoleAssistant
		}
		messages[i] = session.Message{
			AgentFilename: "test.yaml",
			AgentName:     "test",
			Message: chat.Message{
				Role:      role,
				Content:   "Message " + string(rune('A'+i)),
				CreatedAt: time.Now().Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			},
		}
	}
	return messages
}

func TestPaginateMessages_FirstPage(t *testing.T) {
	messages := createTestMessages(100)
	
	params := PaginationParams{
		Limit: 10,
	}
	
	paginated, meta, err := PaginateMessages(messages, params)
	require.NoError(t, err)
	assert.Len(t, paginated, 10)
	assert.Equal(t, 100, meta.TotalMessages)
	assert.Equal(t, 10, meta.Limit)
	assert.True(t, meta.HasMore)
	assert.NotEmpty(t, meta.NextCursor)
	assert.NotEmpty(t, meta.PrevCursor)
	
	// Should get first 10 messages
	assert.Equal(t, "Message A", paginated[0].Message.Content)
	assert.Equal(t, "Message J", paginated[9].Message.Content)
}

func TestPaginateMessages_WithAfterCursor(t *testing.T) {
	messages := createTestMessages(100)
	
	// Get first page
	firstPageParams := PaginationParams{Limit: 10}
	firstPage, firstMeta, err := PaginateMessages(messages, firstPageParams)
	require.NoError(t, err)
	
	// Get second page using nextCursor
	secondPageParams := PaginationParams{
		Limit: 10,
		After: firstMeta.NextCursor,
	}
	secondPage, secondMeta, err := PaginateMessages(messages, secondPageParams)
	require.NoError(t, err)
	
	assert.Len(t, secondPage, 10)
	assert.True(t, secondMeta.HasMore)
	
	// Should get messages 11-20
	assert.Equal(t, "Message K", secondPage[0].Message.Content)
	assert.Equal(t, "Message T", secondPage[9].Message.Content)
	
	// No overlap with first page
	assert.NotEqual(t, firstPage[9].Message.Content, secondPage[0].Message.Content)
}

func TestPaginateMessages_WithBeforeCursor(t *testing.T) {
	messages := createTestMessages(100)
	
	// Get a page in the middle (starting at index 50)
	middleCursor, _ := EncodeCursor(MessageCursor{
		Timestamp: messages[50].Message.CreatedAt,
		Index:     50,
	})
	
	params := PaginationParams{
		Limit:  10,
		Before: middleCursor,
	}
	
	paginated, meta, err := PaginateMessages(messages, params)
	require.NoError(t, err)
	
	assert.Len(t, paginated, 10)
	assert.True(t, meta.HasMore) // There are older messages
	
	// Should get 10 messages before index 50 (indices 40-49)
	assert.Equal(t, "Message "+string(rune('A'+40)), paginated[0].Message.Content)
	assert.Equal(t, "Message "+string(rune('A'+49)), paginated[9].Message.Content)
}

func TestPaginateMessages_DefaultLimit(t *testing.T) {
	messages := createTestMessages(100)
	
	params := PaginationParams{
		Limit: 0, // Should use default
	}
	
	paginated, meta, err := PaginateMessages(messages, params)
	require.NoError(t, err)
	
	assert.Len(t, paginated, DefaultLimit)
	assert.Equal(t, DefaultLimit, meta.Limit)
}

func TestPaginateMessages_MaxLimit(t *testing.T) {
	messages := createTestMessages(300)
	
	params := PaginationParams{
		Limit: 500, // Should be capped at MaxLimit
	}
	
	paginated, meta, err := PaginateMessages(messages, params)
	require.NoError(t, err)
	
	assert.Len(t, paginated, MaxLimit)
	assert.Equal(t, MaxLimit, meta.Limit)
}

func TestPaginateMessages_EmptyMessages(t *testing.T) {
	messages := []session.Message{}
	
	params := PaginationParams{
		Limit: 10,
	}
	
	paginated, meta, err := PaginateMessages(messages, params)
	require.NoError(t, err)
	
	assert.Len(t, paginated, 0)
	assert.Equal(t, 0, meta.TotalMessages)
	assert.False(t, meta.HasMore)
}

func TestPaginateMessages_LastPage(t *testing.T) {
	messages := createTestMessages(25)
	
	// Get first page
	firstPageParams := PaginationParams{Limit: 10}
	_, firstMeta, err := PaginateMessages(messages, firstPageParams)
	require.NoError(t, err)
	
	// Get second page
	secondPageParams := PaginationParams{
		Limit: 10,
		After: firstMeta.NextCursor,
	}
	_, secondMeta, err := PaginateMessages(messages, secondPageParams)
	require.NoError(t, err)
	
	// Get third page (last page, only 5 messages)
	thirdPageParams := PaginationParams{
		Limit: 10,
		After: secondMeta.NextCursor,
	}
	thirdPage, thirdMeta, err := PaginateMessages(messages, thirdPageParams)
	require.NoError(t, err)
	
	assert.Len(t, thirdPage, 5) // Only 5 messages left
	assert.False(t, thirdMeta.HasMore) // No more messages
	assert.Equal(t, 25, thirdMeta.TotalMessages)
}

func TestPaginateMessages_AfterLastMessage(t *testing.T) {
	messages := createTestMessages(10)
	
	// Create cursor pointing to last message
	lastCursor, _ := EncodeCursor(MessageCursor{
		Timestamp: messages[9].Message.CreatedAt,
		Index:     9,
	})
	
	params := PaginationParams{
		Limit: 10,
		After: lastCursor,
	}
	
	paginated, meta, err := PaginateMessages(messages, params)
	require.NoError(t, err)
	
	assert.Len(t, paginated, 0)
	assert.False(t, meta.HasMore)
}

func TestPaginateMessages_BeforeFirstMessage(t *testing.T) {
	messages := createTestMessages(10)
	
	// Create cursor pointing to first message
	firstCursor, _ := EncodeCursor(MessageCursor{
		Timestamp: messages[0].Message.CreatedAt,
		Index:     0,
	})
	
	params := PaginationParams{
		Limit:  10,
		Before: firstCursor,
	}
	
	paginated, meta, err := PaginateMessages(messages, params)
	require.NoError(t, err)
	
	assert.Len(t, paginated, 0)
	assert.False(t, meta.HasMore)
}

func TestPaginateMessages_InvalidCursor(t *testing.T) {
	messages := createTestMessages(10)
	
	params := PaginationParams{
		Limit: 10,
		After: "invalid-cursor",
	}
	
	_, _, err := PaginateMessages(messages, params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid after cursor")
}
