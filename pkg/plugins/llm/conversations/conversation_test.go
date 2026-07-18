package conversations

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTracker(t *testing.T) {
	tracker := NewTracker()
	tracker.idGenerator = &seqGenerator{seq: 0}
	clock := Clock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	// message
	conv1comp1 := tracker.TrackCompletion(Completion{
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "Hello"),
			NewTextMessage("assistant", "Hello! How can I help you today?"),
		},
	})
	require.Equal(t, &Completion{
		ID:        "1",
		ParentID:  "",
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "Hello"),
			NewTextMessage("assistant", "Hello! How can I help you today?"),
		},
	}, conv1comp1)

	// message
	conv1comp2 := tracker.TrackCompletion(Completion{
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "Hello"),
			NewTextMessage("assistant", "Hello! How can I help you today?"),
			NewTextMessage("user", "What model is this?"),
			NewTextMessage("assistant", "This is Skynet T-5000"),
		},
	})
	require.Equal(t, &Completion{
		ID:        "2",
		ParentID:  "1",
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "What model is this?"),
			NewTextMessage("assistant", "This is Skynet T-5000"),
		},
	}, conv1comp2)

	// message
	// thread 1
	conv1comp3 := tracker.TrackCompletion(Completion{
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "Hello"),
			NewTextMessage("assistant", "Hello! How can I help you today?"),
			NewTextMessage("user", "What model is this?"),
			NewTextMessage("assistant", "This is Skynet T-5000"),
			NewTextMessage("user", "That's not good."),
			NewTextMessage("assistant", "Your feedback has been noted."),
		},
	})
	require.Equal(t, &Completion{
		ID:        "3",
		ParentID:  "2",
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "That's not good."),
			NewTextMessage("assistant", "Your feedback has been noted."),
		},
	}, conv1comp3)

	// message
	// thread 2
	// split off a new thread - i.e. start a new conversation with the same history but a different prompt.
	conv2comp1 := tracker.TrackCompletion(Completion{
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "Hello"),
			NewTextMessage("assistant", "Hello! How can I help you today?"),
			NewTextMessage("user", "What model is this?"),
			NewTextMessage("assistant", "This is Skynet T-5000"),
			NewTextMessage("user", "Great.", "Thanks!"),
			NewTextMessage("assistant", "You're welcome!"),
		},
	})
	require.Equal(t, &Completion{
		ID:        "4",
		ParentID:  "2",
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "Great.", "Thanks!"),
			NewTextMessage("assistant", "You're welcome!"),
		},
	}, conv2comp1)

	// message
	// conv 3
	// entirely new conversation
	conv3comp1 := tracker.TrackCompletion(Completion{
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "One"),
			NewTextMessage("user", "Two"),
			NewTextMessage("assistant", "Three"),
			NewTextMessage("user", "Four"),
			NewTextMessage("assistant", "Five"),
			NewTextMessage("user", "Six"),
			NewTextMessage("assistant", "Seven"),
		},
	})
	require.Equal(t, &Completion{
		ID:        "5",
		ParentID:  "",
		Timestamp: clock.Now(),
		Provider:  "skynet",
		Model:     "t-5000",
		Messages: []*Message{
			NewTextMessage("user", "One"),
			NewTextMessage("user", "Two"),
			NewTextMessage("assistant", "Three"),
			NewTextMessage("user", "Four"),
			NewTextMessage("assistant", "Five"),
			NewTextMessage("user", "Six"),
			NewTextMessage("assistant", "Seven"),
		},
	}, conv3comp1)
}

func TestCompletionHash(t *testing.T) {
	require.Equal(t,
		(&Completion{
			Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Provider:  "skynet",
			Model:     "t-5000",
			Messages: []*Message{
				NewTextMessage("user", "Hello, world!"),
			},
		}).Hash(),
		(&Completion{
			Timestamp: time.Date(1337, 1, 1, 0, 0, 0, 0, time.UTC), // timestamp not included in hash
			Provider:  "skynet",
			Model:     "t-5000",
			Messages: []*Message{
				NewTextMessage("user", "Hello, world!"),
			},
		}).Hash(),
	)

	require.NotEqual(t,
		(&Completion{
			Messages: []*Message{
				NewTextMessage("user", "One", "Two"),
			},
		}).Hash(),
		(&Completion{
			Messages: []*Message{
				NewTextMessage("user", "OneTwo"),
			},
		}).Hash(),
	)
}

type Clock time.Time

func (c *Clock) Now() time.Time {
	return time.Time(*c)
}

func (c *Clock) Add(d time.Duration) {
	*c = Clock(c.Now().Add(d))
}

func TestTracker_Stateful(t *testing.T) {
	tracker := NewTracker()
	tracker.idGenerator = &seqGenerator{seq: 0}
	clock := Clock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	comp1 := tracker.TrackCompletion(Completion{
		Timestamp:  clock.Now(),
		Provider:   "skynet",
		Model:      "t-5000",
		ProviderID: "comp1",
		Messages: []*Message{
			NewTextMessage("user", "Hello"),
			NewTextMessage("assistant", "Hello! How can I help you today?"),
		},
	})

	comp2 := tracker.TrackCompletion(Completion{
		Timestamp:        clock.Now(),
		Provider:         "skynet",
		Model:            "t-5000",
		ProviderID:       "comp2",
		ProviderParentID: "comp1",
		Messages: []*Message{
			NewTextMessage("user", "Random number?"),
			NewTextMessage("assistant", "564"),
		},
	})

	require.NotEmpty(t, comp1.ID)
	require.NotEmpty(t, comp2.ID)
	require.Equal(t, comp1.ID, comp2.ParentID)
}
