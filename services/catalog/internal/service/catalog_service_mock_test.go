package catalog

import (
	"context"
	"database/sql"
	"eventix/proto/catalog/pb"
	"eventix/services/catalog/internal/models"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type MockRepository struct {
	MockGetEventByID func(ctx context.Context, eventID uuid.UUID) (models.Event, error)
	MockGetEvents    func(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error)
	MockCreateEvent  func(ctx context.Context, event *models.Event) (models.Event, error)
	MockUpdateEvent  func(ctx context.Context, event *models.Event) (models.Event, error)
	MockDeleteEvent  func(ctx context.Context, eventID uuid.UUID) error
}

func (m *MockRepository) GetEventByID(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
	return m.MockGetEventByID(ctx, eventID)
}
func (m *MockRepository) GetEvents(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
	return m.MockGetEvents(ctx, page, pageSize, searchQuery)
}
func (m *MockRepository) CreateEvent(ctx context.Context, event *models.Event) (models.Event, error) {
	return m.MockCreateEvent(ctx, event)
}
func (m *MockRepository) UpdateEvent(ctx context.Context, event *models.Event) (models.Event, error) {
	return m.MockUpdateEvent(ctx, event)
}
func (m *MockRepository) DeleteEvent(ctx context.Context, eventID uuid.UUID) error {
	return m.MockDeleteEvent(ctx, eventID)
}

func TestCatalogManager_GetEvent_InvalidEventID(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{}, nil
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	t.Run("RandomSymbols", func(t *testing.T) {
		req := &pb.GetEventRequest{
			EventId: "sasasasas124553",
		}

		resp, err := manager.GetEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "invalid event id format", st.Message())
	})

	t.Run("GraterThan36Characters", func(t *testing.T) {
		req := &pb.GetEventRequest{
			EventId: "446b0194-0c41-4fd3-829a-f9b71a8c504b1212",
		}

		resp, err := manager.GetEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "invalid event id format", st.Message())
	})

	t.Run("OneSymbolUUID", func(t *testing.T) {
		req := &pb.GetEventRequest{
			EventId: "4",
		}

		resp, err := manager.GetEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "invalid event id format", st.Message())
	})
}

func TestCatalogManager_GetEvent_SQLNoRowsError(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{}, sql.ErrNoRows
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.GetEventRequest{
		EventId: uuid.New().String(),
	}

	resp, err := manager.GetEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Equal(t, "event not found", st.Message())
}

func TestCatalogManager_GetEvent_DatabaseError(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{}, fmt.Errorf("db query error")
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.GetEventRequest{
		EventId: uuid.New().String(),
	}

	resp, err := manager.GetEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_GetEvent_Success(t *testing.T) {
	expectedID := uuid.New()
	expectedDate := time.Date(2025, 12, 31, 20, 0, 0, 0, time.UTC)

	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             expectedID,
				Title:          "Rock Concert",
				Description:    "Best rock concert of the year",
				Venue:          "Wembley Stadium",
				EventDate:      expectedDate,
				TotalSeats:     10000,
				AvailableSeats: 9500,
				Price:          150.50,
				CreatedAt:      time.Now().Add(-24 * time.Hour),
				UpdatedAt:      time.Now(),
			}, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.GetEventRequest{
		EventId: expectedID.String(),
	}

	resp, err := manager.GetEvent(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Event)

	assert.Equal(t, expectedID.String(), resp.Event.Id)
	assert.Equal(t, "Rock Concert", resp.Event.Title)
	assert.Equal(t, "Best rock concert of the year", resp.Event.Description)
	assert.Equal(t, "Wembley Stadium", resp.Event.Venue)
	assert.Equal(t, expectedDate.Format(time.RFC3339), resp.Event.EventDate)
	assert.Equal(t, int32(10000), resp.Event.TotalSeats)
	assert.Equal(t, int32(9500), resp.Event.AvailableSeats)
	assert.Equal(t, 150.50, resp.Event.Price)
}

func TestCatalogManager_ListEvent_PaginationError(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEvents: func(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
			return []models.Event{}, 0, nil
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	t.Run("PageSizeEquals200", func(t *testing.T) {
		req := &pb.ListEventsRequest{
			Page:        1,
			PageSize:    200,
			SearchQuery: "consert",
		}

		resp, err := manager.ListEvents(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "page_size cannot be greater than 100", st.Message())
	})

	t.Run("PageSizeGraterThan100", func(t *testing.T) {
		req := &pb.ListEventsRequest{
			Page:        1,
			PageSize:    101,
			SearchQuery: "consert",
		}

		resp, err := manager.ListEvents(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "page_size cannot be greater than 100", st.Message())
	})

	t.Run("DefaulValuesForPageSize", func(t *testing.T) {
		req := &pb.ListEventsRequest{
			Page:        0,
			PageSize:    0,
			SearchQuery: "consert",
		}

		expected := &pb.ListEventsResponse{
			Events:     []*pb.Event{},
			TotalCount: 0,
			Page:       1,
			PageSize:   20,
		}

		resp, err := manager.ListEvents(context.Background(), req)
		assert.Equal(t, expected, resp)
		require.NoError(t, err)
	})
}

func TestCatalogManager_ListEvent_DBCountError(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEvents: func(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
			return []models.Event{}, 0, fmt.Errorf("db count error")
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.ListEventsRequest{
		Page:        1,
		PageSize:    50,
		SearchQuery: "consert",
	}

	resp, err := manager.ListEvents(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_ListEvent_DBQueryError(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEvents: func(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
			return []models.Event{}, 0, fmt.Errorf("db query error")
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.ListEventsRequest{
		Page:        1,
		PageSize:    50,
		SearchQuery: "consert",
	}

	resp, err := manager.ListEvents(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_ListEvent_DBScanError(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEvents: func(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
			return []models.Event{}, 0, fmt.Errorf("db scan error")
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.ListEventsRequest{
		Page:        1,
		PageSize:    50,
		SearchQuery: "consert",
	}

	resp, err := manager.ListEvents(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_ListEvent_DBIterationError(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEvents: func(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
			return []models.Event{}, 0, fmt.Errorf("db rows iteration error")
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.ListEventsRequest{
		Page:        1,
		PageSize:    50,
		SearchQuery: "consert",
	}

	resp, err := manager.ListEvents(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_ListEvents_Success(t *testing.T) {
	eventID1 := uuid.New()
	eventID2 := uuid.New()
	eventDate := time.Date(2025, 12, 31, 20, 0, 0, 0, time.UTC)

	mockEvents := []models.Event{
		{
			ID:             eventID1,
			Title:          "Concert 1",
			Description:    "Description 1",
			Venue:          "Venue 1",
			EventDate:      eventDate,
			TotalSeats:     1000,
			AvailableSeats: 900,
			Price:          100.0,
		},
		{
			ID:             eventID2,
			Title:          "Concert 2",
			Description:    "Description 2",
			Venue:          "Venue 2",
			EventDate:      eventDate,
			TotalSeats:     2000,
			AvailableSeats: 1800,
			Price:          200.0,
		},
	}

	mockRepo := &MockRepository{
		MockGetEvents: func(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
			assert.Equal(t, int32(1), page)
			assert.Equal(t, int32(10), pageSize)
			assert.Equal(t, "rock", searchQuery)

			return mockEvents, int32(len(mockEvents)), nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.ListEventsRequest{
		Page:        1,
		PageSize:    10,
		SearchQuery: "rock",
	}

	resp, err := manager.ListEvents(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(2), resp.TotalCount)
	assert.Equal(t, int32(1), resp.Page)
	assert.Equal(t, int32(10), resp.PageSize)
	assert.Len(t, resp.Events, 2)

	assert.Equal(t, eventID1.String(), resp.Events[0].Id)
	assert.Equal(t, "Concert 1", resp.Events[0].Title)
	assert.Equal(t, "Description 1", resp.Events[0].Description)
	assert.Equal(t, "Venue 1", resp.Events[0].Venue)
	assert.Equal(t, eventDate.Format(time.RFC3339), resp.Events[0].EventDate)
	assert.Equal(t, int32(1000), resp.Events[0].TotalSeats)
	assert.Equal(t, int32(900), resp.Events[0].AvailableSeats)
	assert.Equal(t, 100.0, resp.Events[0].Price)

	assert.Equal(t, eventID2.String(), resp.Events[1].Id)
	assert.Equal(t, "Concert 2", resp.Events[1].Title)
}

func TestCatalogManager_ListEvents_EmptyList(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEvents: func(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
			return []models.Event{}, 0, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.ListEventsRequest{
		Page:        1,
		PageSize:    20,
		SearchQuery: "",
	}

	resp, err := manager.ListEvents(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(0), resp.TotalCount)
	assert.NotNil(t, resp.Events)
	assert.Empty(t, resp.Events)
}

func TestCatalogManager_ListEvents_WithPagination(t *testing.T) {
	var mockEvents []models.Event
	for i := 0; i < 5; i++ {
		mockEvents = append(mockEvents, models.Event{
			ID:             uuid.New(),
			Title:          "Event",
			Description:    "Description",
			Venue:          "Venue",
			EventDate:      time.Now().Add(time.Duration(i) * 24 * time.Hour),
			TotalSeats:     100,
			AvailableSeats: 100,
			Price:          50.0,
		})
	}

	mockRepo := &MockRepository{
		MockGetEvents: func(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
			assert.Equal(t, int32(1), page)
			assert.Equal(t, int32(2), pageSize)
			assert.Equal(t, "", searchQuery)

			return mockEvents[:2], int32(5), nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.ListEventsRequest{
		Page:        1,
		PageSize:    2,
		SearchQuery: "",
	}

	resp, err := manager.ListEvents(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(5), resp.TotalCount)
	assert.Equal(t, int32(1), resp.Page)
	assert.Equal(t, int32(2), resp.PageSize)
	assert.Len(t, resp.Events, 2)
}

func TestCatalogManager_CreateEvent_InvalidDates(t *testing.T) {
	mockRepo := &MockRepository{}
	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	baseReq := &pb.CreateEventRequest{
		Title:       "Valid Title",
		Description: "Valid Desc",
		Venue:       "London",
		EventDate:   "2025-06-29T15:30:45.123Z",
		TotalSeats:  12,
		Price:       66,
	}

	tests := []struct {
		name          string
		eventDate     string
		expectedError string
	}{
		{"Random string", "20 september 2020", "invalid event date format"},
		{"DD-MM-YYYY", "22-08-2000", "invalid event date format"},
		{"MM-DD-YYYY", "08-22-2000", "invalid event date format"},
		{"DD/MM/YYYY", "18/09/2001", "invalid event date format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &pb.CreateEventRequest{
				Title:       baseReq.Title,
				Description: baseReq.Description,
				Venue:       baseReq.Venue,
				EventDate:   tt.eventDate,
				TotalSeats:  baseReq.TotalSeats,
				Price:       baseReq.Price,
			}

			resp, err := manager.CreateEvent(context.Background(), req)

			assert.Nil(t, resp)
			require.Error(t, err)

			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, st.Code())
			assert.Equal(t, tt.expectedError, st.Message())
		})
	}
}

func TestCatalogManager_CreateEvent(t *testing.T) {
	mockRepo := &MockRepository{
		MockCreateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			return models.Event{}, nil
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	t.Run("EmptyTitle", func(t *testing.T) {
		req := &pb.CreateEventRequest{
			Title:       "",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2025-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       66,
		}

		resp, err := manager.CreateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "title is required", st.Message())
	})

	t.Run("TitleGraterThan200Symbols", func(t *testing.T) {
		req := &pb.CreateEventRequest{
			Title:       "dfsjdhflhlkhAS';K'LSK;'FDJS;KJD;LFHLHwiuiquwieiqyioy329736497236yiusydifutsiydtfyistiipasodjoaj;dlj;ajs;djk;lhflskdlfgjksgdkjfgfjdhfsgiduyfgisgdifgjhaslJGDKHGAKFGLFHKSDHGLJSLJGLJjlgo2394862396^&%^*%*(&^yhofuhsdfjklhskdgfkjsgljdfhlsfsdf",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2025-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       66,
		}

		resp, err := manager.CreateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "title is too long", st.Message())
	})

	t.Run("PriceIsZero_1", func(t *testing.T) {
		req := &pb.CreateEventRequest{
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2025-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       0.0,
		}

		resp, err := manager.CreateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "price cannot be negative", st.Message())
	})

	t.Run("PriceIsZero_2", func(t *testing.T) {
		req := &pb.CreateEventRequest{
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2025-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       0,
		}

		resp, err := manager.CreateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "price cannot be negative", st.Message())
	})

	t.Run("NegativePrice", func(t *testing.T) {
		req := &pb.CreateEventRequest{
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2025-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       -12.1,
		}

		resp, err := manager.CreateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "price cannot be negative", st.Message())
	})

	t.Run("NegativeTotalSeats", func(t *testing.T) {
		req := &pb.CreateEventRequest{
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2025-06-29T15:30:45.123Z",
			TotalSeats:  -13,
			Price:       55,
		}

		resp, err := manager.CreateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "totalSeats cannot be negative", st.Message())
	})

	t.Run("DateTimeInThePast_1", func(t *testing.T) {
		req := &pb.CreateEventRequest{
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2025-06-29T15:30:45.123Z",
			TotalSeats:  66,
			Price:       55,
		}

		resp, err := manager.CreateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "eventDate cannot be in the past", st.Message())
	})

	t.Run("DateTimeInThePast_2", func(t *testing.T) {
		req := &pb.CreateEventRequest{
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
			TotalSeats:  66,
			Price:       55,
		}

		resp, err := manager.CreateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "eventDate cannot be in the past", st.Message())
	})

	t.Run("DateTimeInThePast_3", func(t *testing.T) {
		req := &pb.CreateEventRequest{
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			TotalSeats:  66,
			Price:       55,
		}

		resp, err := manager.CreateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "eventDate cannot be in the past", st.Message())
	})
}

func TestCatalogManager_CreateEvent_DBDuplicateKeyError(t *testing.T) {
	mockRepo := &MockRepository{
		MockCreateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			return models.Event{}, fmt.Errorf("event already exists: duplicate key")
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.CreateEventRequest{
		Title:       "Welcome back",
		Description: "something strange",
		Venue:       "London",
		EventDate:   time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
		TotalSeats:  66,
		Price:       55,
	}

	resp, err := manager.CreateEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.AlreadyExists, st.Code())
	assert.Equal(t, "event already created", st.Message())
}

func TestCatalogManager_CreateEvent_DBError(t *testing.T) {
	mockRepo := &MockRepository{
		MockCreateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			return models.Event{}, fmt.Errorf("db create error")
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.CreateEventRequest{
		Title:       "Welcome back",
		Description: "something strange",
		Venue:       "London",
		EventDate:   time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
		TotalSeats:  66,
		Price:       55,
	}

	resp, err := manager.CreateEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_CreateEvent_Success(t *testing.T) {
	eventID := uuid.New()
	eventDate := time.Date(2030, 12, 31, 20, 0, 0, 0, time.UTC)
	now := time.Now()

	mockRepo := &MockRepository{
		MockCreateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			assert.Equal(t, "New Concert", event.Title)
			assert.Equal(t, "Amazing show", event.Description)
			assert.Equal(t, "London Arena", event.Venue)
			assert.Equal(t, eventDate, event.EventDate)
			assert.Equal(t, int32(5000), event.TotalSeats)
			assert.Equal(t, int32(5000), event.AvailableSeats)
			assert.Equal(t, 250.0, event.Price)

			return models.Event{
				ID:             eventID,
				Title:          event.Title,
				Description:    event.Description,
				Venue:          event.Venue,
				EventDate:      event.EventDate,
				TotalSeats:     event.TotalSeats,
				AvailableSeats: event.AvailableSeats,
				Price:          event.Price,
				CreatedAt:      now,
				UpdatedAt:      now,
			}, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.CreateEventRequest{
		Title:       "New Concert",
		Description: "Amazing show",
		Venue:       "London Arena",
		EventDate:   eventDate.Format(time.RFC3339),
		TotalSeats:  5000,
		Price:       250.0,
	}

	resp, err := manager.CreateEvent(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Event)

	assert.Equal(t, eventID.String(), resp.Event.Id)
	assert.Equal(t, "New Concert", resp.Event.Title)
	assert.Equal(t, "Amazing show", resp.Event.Description)
	assert.Equal(t, "London Arena", resp.Event.Venue)
	assert.Equal(t, eventDate.Format(time.RFC3339), resp.Event.EventDate)
	assert.Equal(t, int32(5000), resp.Event.TotalSeats)
	assert.Equal(t, int32(5000), resp.Event.AvailableSeats)
	assert.Equal(t, 250.0, resp.Event.Price)
}

func TestCatalogManager_UpdateEvent_InvalidDates(t *testing.T) {
	eventID := uuid.New()
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             eventID,
				Title:          "Existing Event",
				Description:    "Existing Description",
				Venue:          "Existing Venue",
				EventDate:      time.Date(2030, 1, 1, 20, 0, 0, 0, time.UTC),
				TotalSeats:     100,
				AvailableSeats: 100,
				Price:          50.0,
			}, nil
		},
	}
	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	baseReq := &pb.UpdateEventRequest{
		EventId:     eventID.String(),
		Title:       "Valid Update Title",
		Description: "Valid Update Desc",
		Venue:       "Dallas",
		EventDate:   "2025-06-29T15:30:45.123Z",
		TotalSeats:  12,
		Price:       66,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
		},
	}

	tests := []struct {
		name          string
		eventDate     string
		expectedError string
	}{
		{"Random string", "20 september 2020", "invalid event date format"},
		{"DD-MM-YYYY", "22-08-2000", "invalid event date format"},
		{"MM-DD-YYYY", "08-22-2000", "invalid event date format"},
		{"DD/MM/YYYY", "18/09/2001", "invalid event date format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &pb.UpdateEventRequest{
				EventId:     baseReq.EventId,
				Title:       baseReq.Title,
				Description: baseReq.Description,
				Venue:       baseReq.Venue,
				EventDate:   tt.eventDate,
				TotalSeats:  baseReq.TotalSeats,
				Price:       baseReq.Price,
				UpdateMask:  baseReq.UpdateMask,
			}
			resp, err := manager.UpdateEvent(context.Background(), req)
			assert.Nil(t, resp)
			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, st.Code())
			assert.Equal(t, tt.expectedError, st.Message())
		})
	}
}

func TestCatalogManager_UpdateEvent(t *testing.T) {
	eventID := uuid.New()
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             eventID,
				Title:          "Existing Event",
				Description:    "Existing Description",
				Venue:          "Existing Venue",
				EventDate:      time.Date(2030, 1, 1, 20, 0, 0, 0, time.UTC),
				TotalSeats:     100,
				AvailableSeats: 100,
				Price:          50.0,
			}, nil
		},
	}
	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	t.Run("EmptyTitle", func(t *testing.T) {
		req := &pb.UpdateEventRequest{
			EventId:     eventID.String(),
			Title:       "",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2035-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       66,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
			},
		}
		resp, err := manager.UpdateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "title is required", st.Message())
	})

	t.Run("TitleGraterThan200Symbols", func(t *testing.T) {
		req := &pb.UpdateEventRequest{
			EventId:     eventID.String(),
			Title:       "dfsjdhflhlkhAS';K'LSK;'FDJS;KJD;LFHLHwiuiquwieiqyioy329736497236yiusydifutsiydtfyistiipasodjoaj;dlj;ajs;djk;lhflskdlfgjksgdkjfgfjdhfsgiduyfgisgdifgjhaslJGDKHGAKFGLFHKSDHGLJSLJGLJjlgo2394862396^&%^*%*(&^yhofuhsdfjklhskdgfkjsgljdfhlsfsdf",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2035-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       66,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
			},
		}
		resp, err := manager.UpdateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "title is too long", st.Message())
	})

	t.Run("PriceIsZero_1", func(t *testing.T) {
		req := &pb.UpdateEventRequest{
			EventId:     eventID.String(),
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2035-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       0.0,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
			},
		}
		resp, err := manager.UpdateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "price cannot be negative", st.Message())
	})

	t.Run("PriceIsZero_2", func(t *testing.T) {
		req := &pb.UpdateEventRequest{
			EventId:     eventID.String(),
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2035-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       0,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
			},
		}
		resp, err := manager.UpdateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "price cannot be negative", st.Message())
	})

	t.Run("NegativePrice", func(t *testing.T) {
		req := &pb.UpdateEventRequest{
			EventId:     eventID.String(),
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2035-06-29T15:30:45.123Z",
			TotalSeats:  12,
			Price:       -12.1,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
			},
		}
		resp, err := manager.UpdateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "price cannot be negative", st.Message())
	})

	t.Run("NegativeTotalSeats", func(t *testing.T) {
		req := &pb.UpdateEventRequest{
			EventId:     eventID.String(),
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "2035-06-29T15:30:45.123Z",
			TotalSeats:  -13,
			Price:       55,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
			},
		}
		resp, err := manager.UpdateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "totalSeats cannot be negative", st.Message())
	})

	t.Run("DateInThePast", func(t *testing.T) {
		req := &pb.UpdateEventRequest{
			EventId:     eventID.String(),
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
			TotalSeats:  66,
			Price:       55,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
			},
		}
		resp, err := manager.UpdateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "eventDate cannot be in the past", st.Message())
	})

	t.Run("InvalidEventID", func(t *testing.T) {
		req := &pb.UpdateEventRequest{
			EventId:     "asasafdsfsgsdfg",
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
			TotalSeats:  66,
			Price:       55,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
			},
		}
		resp, err := manager.UpdateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "invalid event id format", st.Message())
	})

	t.Run("EmptyEventDate", func(t *testing.T) {
		req := &pb.UpdateEventRequest{
			EventId:     eventID.String(),
			Title:       "Welcome back",
			Description: "something strange",
			Venue:       "London",
			EventDate:   "",
			TotalSeats:  66,
			Price:       55,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
			},
		}
		resp, err := manager.UpdateEvent(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "event_date is required", st.Message())
	})
}

func TestCatalogManager_UpdateEvent_DBNoRowsError(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{}, sql.ErrNoRows
		},
	}
	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.UpdateEventRequest{
		EventId:     uuid.NewString(),
		Title:       "Welcome back",
		Description: "something strange",
		Venue:       "London",
		EventDate:   "2035-06-29T15:30:45.123Z",
		TotalSeats:  66,
		Price:       55,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
		},
	}
	resp, err := manager.UpdateEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Equal(t, "event not found", st.Message())
}

func TestCatalogManager_UpdateEvent_DBError(t *testing.T) {
	eventID := uuid.New()
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             eventID,
				Title:          "Existing",
				Description:    "Existing",
				Venue:          "Existing",
				EventDate:      time.Date(2030, 1, 1, 20, 0, 0, 0, time.UTC),
				TotalSeats:     100,
				AvailableSeats: 100,
				Price:          50.0,
			}, nil
		},
		MockUpdateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			return models.Event{}, fmt.Errorf("db query error")
		},
	}
	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.UpdateEventRequest{
		EventId:     eventID.String(),
		Title:       "Welcome back",
		Description: "something strange",
		Venue:       "London",
		EventDate:   "2035-06-29T15:30:45.123Z",
		TotalSeats:  66,
		Price:       55,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
		},
	}
	resp, err := manager.UpdateEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_UpdateEvent_Success(t *testing.T) {
	eventID := uuid.New()
	eventDate := time.Date(2030, 12, 31, 20, 0, 0, 0, time.UTC)
	now := time.Now()

	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             eventID,
				Title:          "Old Title",
				Description:    "Old Description",
				Venue:          "Old Venue",
				EventDate:      time.Date(2029, 1, 1, 20, 0, 0, 0, time.UTC),
				TotalSeats:     1000,
				AvailableSeats: 900,
				Price:          100.0,
			}, nil
		},
		MockUpdateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			assert.Equal(t, eventID, event.ID)
			assert.Equal(t, "Updated Concert", event.Title)
			assert.Equal(t, "Updated description", event.Description)
			assert.Equal(t, "New Venue", event.Venue)
			assert.Equal(t, eventDate, event.EventDate)
			assert.Equal(t, int32(3000), event.TotalSeats)
			assert.Equal(t, 180.0, event.Price)
			return models.Event{
				ID:             eventID,
				Title:          event.Title,
				Description:    event.Description,
				Venue:          event.Venue,
				EventDate:      event.EventDate,
				TotalSeats:     event.TotalSeats,
				AvailableSeats: 2500,
				Price:          event.Price,
				CreatedAt:      now.Add(-48 * time.Hour),
				UpdatedAt:      now,
			}, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.UpdateEventRequest{
		EventId:     eventID.String(),
		Title:       "Updated Concert",
		Description: "Updated description",
		Venue:       "New Venue",
		EventDate:   eventDate.Format(time.RFC3339),
		TotalSeats:  3000,
		Price:       180.0,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price"},
		},
	}

	resp, err := manager.UpdateEvent(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Event)
	assert.Equal(t, eventID.String(), resp.Event.Id)
	assert.Equal(t, "Updated Concert", resp.Event.Title)
	assert.Equal(t, "Updated description", resp.Event.Description)
	assert.Equal(t, "New Venue", resp.Event.Venue)
	assert.Equal(t, eventDate.Format(time.RFC3339), resp.Event.EventDate)
	assert.Equal(t, int32(3000), resp.Event.TotalSeats)
	assert.Equal(t, int32(2500), resp.Event.AvailableSeats)
	assert.Equal(t, 180.0, resp.Event.Price)
}

func TestCatalogManager_UpdateEvent_WithFieldMask_PartialUpdate(t *testing.T) {
	eventID := uuid.New()
	now := time.Now()

	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             eventID,
				Title:          "Old Title",
				Description:    "Old Description",
				Venue:          "Old Venue",
				EventDate:      time.Date(2029, 1, 1, 20, 0, 0, 0, time.UTC),
				TotalSeats:     1000,
				AvailableSeats: 900,
				Price:          100.0,
			}, nil
		},
		MockUpdateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			assert.Equal(t, "New Title Only", event.Title)
			assert.Equal(t, "Old Description", event.Description)
			assert.Equal(t, "Old Venue", event.Venue)
			assert.Equal(t, int32(2000), event.TotalSeats)
			assert.Equal(t, 100.0, event.Price)
			return models.Event{
				ID:             eventID,
				Title:          event.Title,
				Description:    event.Description,
				Venue:          event.Venue,
				EventDate:      event.EventDate,
				TotalSeats:     event.TotalSeats,
				AvailableSeats: 900,
				Price:          event.Price,
				CreatedAt:      now.Add(-48 * time.Hour),
				UpdatedAt:      now,
			}, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.UpdateEventRequest{
		EventId:    eventID.String(),
		Title:      "New Title Only",
		TotalSeats: 2000,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"title", "total_seats"},
		},
	}

	resp, err := manager.UpdateEvent(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "New Title Only", resp.Event.Title)
	assert.Equal(t, "Old Description", resp.Event.Description)
	assert.Equal(t, "Old Venue", resp.Event.Venue)
	assert.Equal(t, int32(2000), resp.Event.TotalSeats)
	assert.Equal(t, 100.0, resp.Event.Price)
}

func TestCatalogManager_UpdateEvent_WithFieldMask_OnlyDescription(t *testing.T) {
	eventID := uuid.New()
	now := time.Now()

	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             eventID,
				Title:          "Keep This Title",
				Description:    "Old Description",
				Venue:          "Keep This Venue",
				EventDate:      time.Date(2029, 1, 1, 20, 0, 0, 0, time.UTC),
				TotalSeats:     1000,
				AvailableSeats: 900,
				Price:          100.0,
			}, nil
		},
		MockUpdateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			assert.Equal(t, "Keep This Title", event.Title)
			assert.Equal(t, "New Description Only", event.Description)
			assert.Equal(t, "Keep This Venue", event.Venue)
			return models.Event{
				ID:             eventID,
				Title:          event.Title,
				Description:    event.Description,
				Venue:          event.Venue,
				EventDate:      event.EventDate,
				TotalSeats:     event.TotalSeats,
				AvailableSeats: 900,
				Price:          event.Price,
				CreatedAt:      now.Add(-48 * time.Hour),
				UpdatedAt:      now,
			}, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.UpdateEventRequest{
		EventId:     eventID.String(),
		Description: "New Description Only",
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"description"},
		},
	}

	resp, err := manager.UpdateEvent(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "Keep This Title", resp.Event.Title)
	assert.Equal(t, "New Description Only", resp.Event.Description)
	assert.Equal(t, "Keep This Venue", resp.Event.Venue)
}

func TestCatalogManager_UpdateEvent_WithFieldMask_UnknownField(t *testing.T) {
	eventID := uuid.New()
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             eventID,
				Title:          "Existing",
				Description:    "Existing",
				Venue:          "Existing",
				EventDate:      time.Date(2030, 1, 1, 20, 0, 0, 0, time.UTC),
				TotalSeats:     100,
				AvailableSeats: 100,
				Price:          50.0,
			}, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.UpdateEventRequest{
		EventId: eventID.String(),
		Title:   "New Title",
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"title", "nonexistent_field"},
		},
	}

	resp, err := manager.UpdateEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "unknown field")
}

func TestCatalogManager_UpdateEvent_WithFieldMask_EmptyMask(t *testing.T) {
	eventID := uuid.New()
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             eventID,
				Title:          "Existing",
				Description:    "Existing",
				Venue:          "Existing",
				EventDate:      time.Date(2030, 1, 1, 20, 0, 0, 0, time.UTC),
				TotalSeats:     100,
				AvailableSeats: 100,
				Price:          50.0,
			}, nil
		},
		MockUpdateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			return models.Event{}, fmt.Errorf("empty update mask")
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.UpdateEventRequest{
		EventId:     eventID.String(),
		Title:       "New Title",
		Description: "New Desc",
		Venue:       "New Venue",
		EventDate:   "2035-06-29T15:30:45.123Z",
		TotalSeats:  200,
		Price:       75.0,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{},
		},
	}

	resp, err := manager.UpdateEvent(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestCatalogManager_UpdateEvent_WithoutFieldMask_FullUpdate(t *testing.T) {
	eventID := uuid.New()
	eventDate := time.Date(2030, 12, 31, 20, 0, 0, 0, time.UTC)
	now := time.Now()

	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{
				ID:             eventID,
				Title:          "Old Title",
				Description:    "Old Description",
				Venue:          "Old Venue",
				EventDate:      time.Date(2029, 1, 1, 20, 0, 0, 0, time.UTC),
				TotalSeats:     1000,
				AvailableSeats: 900,
				Price:          100.0,
			}, nil
		},
		MockUpdateEvent: func(ctx context.Context, event *models.Event) (models.Event, error) {
			assert.Equal(t, "Full Update Title", event.Title)
			assert.Equal(t, "Full Update Desc", event.Description)
			assert.Equal(t, "Full Update Venue", event.Venue)
			assert.Equal(t, eventDate, event.EventDate)
			assert.Equal(t, int32(5000), event.TotalSeats)
			assert.Equal(t, 250.0, event.Price)
			return models.Event{
				ID:             eventID,
				Title:          event.Title,
				Description:    event.Description,
				Venue:          event.Venue,
				EventDate:      event.EventDate,
				TotalSeats:     event.TotalSeats,
				AvailableSeats: 4500,
				Price:          event.Price,
				CreatedAt:      now.Add(-48 * time.Hour),
				UpdatedAt:      now,
			}, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.UpdateEventRequest{
		EventId:     eventID.String(),
		Title:       "Full Update Title",
		Description: "Full Update Desc",
		Venue:       "Full Update Venue",
		EventDate:   eventDate.Format(time.RFC3339),
		TotalSeats:  5000,
		Price:       250.0,
	}

	resp, err := manager.UpdateEvent(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "Full Update Title", resp.Event.Title)
	assert.Equal(t, "Full Update Desc", resp.Event.Description)
	assert.Equal(t, "Full Update Venue", resp.Event.Venue)
	assert.Equal(t, eventDate.Format(time.RFC3339), resp.Event.EventDate)
	assert.Equal(t, int32(5000), resp.Event.TotalSeats)
	assert.Equal(t, 250.0, resp.Event.Price)
}

func TestCatalogManager_UpdateEvent_WithFieldMask_GetEventDBError(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetEventByID: func(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
			return models.Event{}, fmt.Errorf("db query error")
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.UpdateEventRequest{
		EventId: uuid.NewString(),
		Title:   "New Title",
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"title"},
		},
	}

	resp, err := manager.UpdateEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_DeleteEvent(t *testing.T) {
	mockRepo := &MockRepository{
		MockDeleteEvent: func(ctx context.Context, eventID uuid.UUID) error {
			return nil
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.DeleteEventRequest{
		EventId: "12343-fdfssa-34234-asasa",
	}

	resp, err := manager.DeleteEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "invalid event id format", st.Message())
}

func TestCatalogManager_DeleteEvent_DBNoRowsError(t *testing.T) {
	mockRepo := &MockRepository{
		MockDeleteEvent: func(ctx context.Context, eventID uuid.UUID) error {
			return sql.ErrNoRows
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.DeleteEventRequest{
		EventId: uuid.NewString(),
	}

	resp, err := manager.DeleteEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Equal(t, "event not found", st.Message())
}

func TestCatalogManager_DeleteEvent_DBError(t *testing.T) {
	mockRepo := &MockRepository{
		MockDeleteEvent: func(ctx context.Context, eventID uuid.UUID) error {
			return fmt.Errorf("db delete error")
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.DeleteEventRequest{
		EventId: uuid.NewString(),
	}

	resp, err := manager.DeleteEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_DeleteEvent_DBError_2(t *testing.T) {
	mockRepo := &MockRepository{
		MockDeleteEvent: func(ctx context.Context, eventID uuid.UUID) error {
			return fmt.Errorf("failed to get rows affected")
		},
	}
	logger := slog.New(slog.DiscardHandler)

	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.DeleteEventRequest{
		EventId: uuid.NewString(),
	}

	resp, err := manager.DeleteEvent(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal server error", st.Message())
}

func TestCatalogManager_DeleteEvent_Success(t *testing.T) {
	mockRepo := &MockRepository{
		MockDeleteEvent: func(ctx context.Context, eventID uuid.UUID) error {
			return nil
		},
	}
	logger := slog.New(slog.DiscardHandler)
	manager := NewCatalogManager(mockRepo, logger)

	req := &pb.DeleteEventRequest{EventId: uuid.New().String()}
	resp, err := manager.DeleteEvent(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}
