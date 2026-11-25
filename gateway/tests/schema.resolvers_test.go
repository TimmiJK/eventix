package tests

import (
	"context"
	"errors"
	"eventix/gateway/graph"
	"eventix/gateway/graph/model"
	grpcclients "eventix/gateway/internal/grpc_clients"
	authPb "eventix/proto/auth/pb"
	catalogPb "eventix/proto/catalog/pb"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type MockAuthClient struct {
	MockRegister      func(ctx context.Context, in *authPb.RegisterRequest, opts ...grpc.CallOption) (*authPb.RegisterResponse, error)
	MockLogin         func(ctx context.Context, in *authPb.LoginRequest, opts ...grpc.CallOption) (*authPb.LoginResponse, error)
	MockLogout        func(ctx context.Context, in *authPb.LogoutRequest, opts ...grpc.CallOption) (*authPb.LogoutResponse, error)
	MockRefreshToken  func(ctx context.Context, in *authPb.RefreshTokenRequest, opts ...grpc.CallOption) (*authPb.RefreshTokenResponse, error)
	MockValidateToken func(ctx context.Context, in *authPb.ValidateTokenRequest, opts ...grpc.CallOption) (*authPb.ValidateTokenResponse, error)
	MockGetMe         func(ctx context.Context, in *authPb.GetMeRequest, opts ...grpc.CallOption) (*authPb.GetMeResponse, error)
}

func (m *MockAuthClient) Register(ctx context.Context, in *authPb.RegisterRequest, opts ...grpc.CallOption) (*authPb.RegisterResponse, error) {
	return m.MockRegister(ctx, in, opts...)
}

func (m *MockAuthClient) Login(ctx context.Context, in *authPb.LoginRequest, opts ...grpc.CallOption) (*authPb.LoginResponse, error) {
	return m.MockLogin(ctx, in, opts...)
}

func (m *MockAuthClient) Logout(ctx context.Context, in *authPb.LogoutRequest, opts ...grpc.CallOption) (*authPb.LogoutResponse, error) {
	return m.MockLogout(ctx, in, opts...)
}

func (m *MockAuthClient) RefreshToken(ctx context.Context, in *authPb.RefreshTokenRequest, opts ...grpc.CallOption) (*authPb.RefreshTokenResponse, error) {
	return m.MockRefreshToken(ctx, in, opts...)
}

func (m *MockAuthClient) ValidateToken(ctx context.Context, in *authPb.ValidateTokenRequest, opts ...grpc.CallOption) (*authPb.ValidateTokenResponse, error) {
	return m.MockValidateToken(ctx, in, opts...)
}

func (m *MockAuthClient) GetMe(ctx context.Context, in *authPb.GetMeRequest, opts ...grpc.CallOption) (*authPb.GetMeResponse, error) {
	return m.MockGetMe(ctx, in, opts...)
}

type MockCatalogClient struct {
	MockGetEvent    func(ctx context.Context, in *catalogPb.GetEventRequest, opts ...grpc.CallOption) (*catalogPb.GetEventResponse, error)
	MockListEvents  func(ctx context.Context, in *catalogPb.ListEventsRequest, opts ...grpc.CallOption) (*catalogPb.ListEventsResponse, error)
	MockCreateEvent func(ctx context.Context, in *catalogPb.CreateEventRequest, opts ...grpc.CallOption) (*catalogPb.CreateEventResponse, error)
	MockUpdateEvent func(ctx context.Context, in *catalogPb.UpdateEventRequest, opts ...grpc.CallOption) (*catalogPb.UpdateEventResponse, error)
	MockDeleteEvent func(ctx context.Context, in *catalogPb.DeleteEventRequest, opts ...grpc.CallOption) (*catalogPb.DeleteEventResponse, error)
}

func (m *MockCatalogClient) GetEvent(ctx context.Context, in *catalogPb.GetEventRequest, opts ...grpc.CallOption) (*catalogPb.GetEventResponse, error) {
	return m.MockGetEvent(ctx, in, opts...)
}

func (m *MockCatalogClient) ListEvents(ctx context.Context, in *catalogPb.ListEventsRequest, opts ...grpc.CallOption) (*catalogPb.ListEventsResponse, error) {
	return m.MockListEvents(ctx, in, opts...)
}

func (m *MockCatalogClient) CreateEvent(ctx context.Context, in *catalogPb.CreateEventRequest, opts ...grpc.CallOption) (*catalogPb.CreateEventResponse, error) {
	return m.MockCreateEvent(ctx, in, opts...)
}

func (m *MockCatalogClient) UpdateEvent(ctx context.Context, in *catalogPb.UpdateEventRequest, opts ...grpc.CallOption) (*catalogPb.UpdateEventResponse, error) {
	return m.MockUpdateEvent(ctx, in, opts...)
}

func (m *MockCatalogClient) DeleteEvent(ctx context.Context, in *catalogPb.DeleteEventRequest, opts ...grpc.CallOption) (*catalogPb.DeleteEventResponse, error) {
	return m.MockDeleteEvent(ctx, in, opts...)
}

func TestAuthResolver_Success(t *testing.T) {
	mockAuth := &MockAuthClient{
		MockRegister: func(ctx context.Context, in *authPb.RegisterRequest, opts ...grpc.CallOption) (*authPb.RegisterResponse, error) {
			assert.Equal(t, "test@example.com", in.Email)
			assert.Equal(t, "password123", in.Password)
			assert.Equal(t, "Test User", in.Name)

			return &authPb.RegisterResponse{
				UserId:       "user-123",
				AccessToken:  "access-token",
				RefreshToken: "refresh-token",
				ExpiresIn:    900,
			}, nil
		},
		MockLogin: func(ctx context.Context, in *authPb.LoginRequest, opts ...grpc.CallOption) (*authPb.LoginResponse, error) {
			assert.Equal(t, "test@example.com", in.Email)
			assert.Equal(t, "password123", in.Password)

			return &authPb.LoginResponse{
				UserId:       "user-123",
				AccessToken:  "access-token123",
				RefreshToken: "refresh-token123",
				ExpiresIn:    900,
			}, nil
		},

		MockLogout: func(ctx context.Context, in *authPb.LogoutRequest, opts ...grpc.CallOption) (*authPb.LogoutResponse, error) {
			assert.Equal(t, "refresh-token123", in.RefreshToken)

			return &authPb.LogoutResponse{
				Success: true,
			}, nil
		},
	}

	mockCatalog := &MockCatalogClient{}
	resolver := &graph.Resolver{
		Clients: &grpcclients.Clients{
			Auth:    mockAuth,
			Catalog: mockCatalog,
		},
	}
	t.Run("Register", func(t *testing.T) {
		registerInput := model.RegisterInput{
			Email:    "test@example.com",
			Password: "password123",
			Name:     "Test User",
		}

		result, err := resolver.Mutation().Register(context.Background(), registerInput)

		require.NoError(t, err)
		assert.Equal(t, "user-123", result.UserID)
		assert.Equal(t, "access-token", result.AccessToken)
		assert.Equal(t, "refresh-token", result.RefreshToken)
		assert.Equal(t, int32(900), result.ExpiresIn)
	})

	t.Run("Login", func(t *testing.T) {
		loginInput := model.LoginInput{
			Email:    "test@example.com",
			Password: "password123",
		}

		result, err := resolver.Mutation().Login(context.Background(), loginInput)
		require.NoError(t, err)
		assert.Equal(t, "user-123", result.UserID)
		assert.Equal(t, "access-token123", result.AccessToken)
		assert.Equal(t, "refresh-token123", result.RefreshToken)
		assert.Equal(t, int32(900), result.ExpiresIn)
	})

	t.Run("Logout", func(t *testing.T) {
		result, err := resolver.Mutation().Logout(context.Background(), "refresh-token123")
		require.NoError(t, err)
		assert.True(t, result)
	})
}

func TestAuthResolver_Error(t *testing.T) {
	mockAuth := &MockAuthClient{
		MockRegister: func(ctx context.Context, in *authPb.RegisterRequest, opts ...grpc.CallOption) (*authPb.RegisterResponse, error) {
			assert.Equal(t, "test@user.com", in.Email)
			assert.Equal(t, "pass12445667Word", in.Password)
			assert.Equal(t, "Common User", in.Name)

			return nil, errors.New("registration failed")
		},
		MockLogin: func(ctx context.Context, in *authPb.LoginRequest, opts ...grpc.CallOption) (*authPb.LoginResponse, error) {
			assert.Equal(t, "test@user.com", in.Email)
			assert.Equal(t, "pass12445667Word", in.Password)

			return nil, errors.New("login failed")
		},
		MockLogout: func(ctx context.Context, in *authPb.LogoutRequest, opts ...grpc.CallOption) (*authPb.LogoutResponse, error) {
			assert.Equal(t, "refresh-token123", in.RefreshToken)

			return nil, errors.New("logout failed")
		},
	}

	mockCatalog := &MockCatalogClient{}
	resolver := &graph.Resolver{
		Clients: &grpcclients.Clients{
			Auth:    mockAuth,
			Catalog: mockCatalog,
		},
	}
	t.Run("RegisterError", func(t *testing.T) {
		registerInput := model.RegisterInput{
			Email:    "test@user.com",
			Password: "pass12445667Word",
			Name:     "Common User",
		}

		result, err := resolver.Mutation().Register(context.Background(), registerInput)

		require.Error(t, err)
		assert.Empty(t, result)
		assert.Contains(t, err.Error(), "registration failed")
	})

	t.Run("LoginError", func(t *testing.T) {
		loginInput := model.LoginInput{
			Email:    "test@user.com",
			Password: "pass12445667Word",
		}

		result, err := resolver.Mutation().Login(context.Background(), loginInput)

		require.Error(t, err)
		assert.Empty(t, result)
		assert.Equal(t, "input: login failed", err.Error())
	})

	t.Run("LogoutError", func(t *testing.T) {
		result, err := resolver.Mutation().Logout(context.Background(), "refresh-token123")

		require.Error(t, err)
		assert.False(t, result)
		assert.Contains(t, err.Error(), "logout failed")
	})
}

func catalogResolverContext() context.Context {
	return context.WithValue(context.Background(), "accessTokenKey", "test-access-token")
}

func TestCatalogResolvers_Success(t *testing.T) {
	assertBearer := func(ctx context.Context) {
		md, ok := metadata.FromOutgoingContext(ctx)
		assert.True(t, ok)
		assert.Equal(t, []string{"Bearer test-access-token"}, md.Get("authorization"))
	}
	mockAuth := &MockAuthClient{}
	mockCatalog := &MockCatalogClient{
		MockCreateEvent: func(ctx context.Context, in *catalogPb.CreateEventRequest, opts ...grpc.CallOption) (*catalogPb.CreateEventResponse, error) {
			assertBearer(ctx)
			return &catalogPb.CreateEventResponse{
				Event: &catalogPb.Event{
					Id:             "newID-12355",
					Title:          in.Title,
					Description:    in.Description,
					Venue:          in.Venue,
					EventDate:      in.EventDate,
					TotalSeats:     in.TotalSeats,
					AvailableSeats: in.TotalSeats,
					PriceMinor:     in.PriceMinor,
					Currency:       in.Currency,
					Version:        1,
				},
			}, nil
		},
		MockUpdateEvent: func(ctx context.Context, in *catalogPb.UpdateEventRequest, opts ...grpc.CallOption) (*catalogPb.UpdateEventResponse, error) {
			assertBearer(ctx)
			assert.Empty(t, in.Title)
			assert.Empty(t, in.Description)
			assert.Empty(t, in.EventDate)
			assert.Zero(t, in.PriceMinor)
			assert.Equal(t, int64(1), in.ExpectedVersion)
			assert.Equal(t, []string{"venue", "total_seats"}, in.GetUpdateMask().GetPaths())

			return &catalogPb.UpdateEventResponse{
				Event: &catalogPb.Event{
					Id:             in.EventId,
					Title:          "New Event",
					Description:    "Something about event",
					Venue:          in.Venue,
					EventDate:      "2026-05-05T21:00:00Z",
					TotalSeats:     in.TotalSeats,
					AvailableSeats: in.TotalSeats,
					PriceMinor:     9999,
					Currency:       "RUB",
					Version:        2,
				},
			}, nil
		},
		MockDeleteEvent: func(ctx context.Context, in *catalogPb.DeleteEventRequest, opts ...grpc.CallOption) (*catalogPb.DeleteEventResponse, error) {
			assertBearer(ctx)
			assert.Equal(t, "newID-12355", in.EventId)

			return &catalogPb.DeleteEventResponse{
				Success: true,
			}, nil
		},
		MockGetEvent: func(ctx context.Context, in *catalogPb.GetEventRequest, opts ...grpc.CallOption) (*catalogPb.GetEventResponse, error) {
			assert.Equal(t, "newID-12355", in.EventId)

			return &catalogPb.GetEventResponse{
				Event: &catalogPb.Event{
					Id:             in.EventId,
					Title:          "New Event",
					Description:    "Something about event",
					Venue:          "Los-Angeles",
					EventDate:      "2026-05-05T21:00:00Z",
					TotalSeats:     150,
					AvailableSeats: 150,
					PriceMinor:     9999,
				},
			}, nil
		},
	}

	resolver := &graph.Resolver{
		Clients: &grpcclients.Clients{
			Auth:    mockAuth,
			Catalog: mockCatalog,
		},
	}

	var eventID string
	title := "New Event"
	description := "Something about event"
	venue := "Los-Angeles"
	eventDate := time.Date(2026, time.May, 5, 21, 00, 00, 00, time.UTC)
	totalSeats := 150
	price := int64(9999)
	t.Run("CreateEvent", func(t *testing.T) {
		createEventInput := model.CreateEventInput{
			Title:       title,
			Description: &description,
			Venue:       venue,
			EventDate:   eventDate,
			TotalSeats:  int32(totalSeats),
			PriceMinor:  price,
			Currency:    "RUB",
		}

		ctx := catalogResolverContext()
		result, err := resolver.Mutation().CreateEvent(ctx, createEventInput)

		require.NoError(t, err)
		assert.Equal(t, "newID-12355", result.ID)
		assert.Equal(t, title, result.Title)
		assert.Equal(t, &description, result.Description)
		assert.Equal(t, venue, result.Venue)
		assert.Equal(t, eventDate, result.EventDate)
		assert.Equal(t, int32(totalSeats), result.TotalSeats)
		assert.Equal(t, int32(totalSeats), result.AvailableSeats)
		assert.Equal(t, price, result.PriceMinor)

		assert.Equal(t, int64(1), result.Version)
		assert.Equal(t, "RUB", result.Currency)
		eventID = result.ID
	})

	t.Run("UpdateEvent", func(t *testing.T) {
		newVenue := "Washington"
		newTotalSeats := int32(500)

		updateEventInput := model.UpdateEventInput{
			Title:           &title,
			Description:     &description,
			Venue:           &newVenue,
			EventDate:       &eventDate,
			TotalSeats:      &newTotalSeats,
			PriceMinor:      &price,
			ExpectedVersion: 1,
			UpdateMask:      []model.EventField{"venue", "totalSeats"},
		}

		ctx := catalogResolverContext()
		result, err := resolver.Mutation().UpdateEvent(ctx, eventID, updateEventInput)

		require.NoError(t, err)
		assert.Equal(t, eventID, result.ID)
		assert.Equal(t, title, result.Title)
		assert.Equal(t, &description, result.Description)
		assert.Equal(t, newVenue, result.Venue)
		assert.Equal(t, eventDate, result.EventDate)
		assert.Equal(t, newTotalSeats, result.TotalSeats)
		assert.Equal(t, newTotalSeats, result.AvailableSeats)
		assert.Equal(t, price, result.PriceMinor)
	})

	t.Run("DeleteEvent", func(t *testing.T) {
		ctx := catalogResolverContext()
		result, err := resolver.Mutation().DeleteEvent(ctx, eventID, 2)

		require.NoError(t, err)
		assert.True(t, result)
	})
}

func TestCatalogResolvers_UpdateEvent_InfersMaskAndFormatsDate(t *testing.T) {
	eventDate := time.Date(2030, time.January, 1, 21, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	description := ""
	client := &MockCatalogClient{
		MockUpdateEvent: func(ctx context.Context, in *catalogPb.UpdateEventRequest, opts ...grpc.CallOption) (*catalogPb.UpdateEventResponse, error) {
			assert.Equal(t, int64(7), in.GetExpectedVersion())
			assert.Equal(t, []string{"description", "event_date"}, in.GetUpdateMask().GetPaths())
			assert.Equal(t, "2030-01-01T21:00:00+03:00", in.GetEventDate())
			assert.Empty(t, in.GetDescription())
			return &catalogPb.UpdateEventResponse{
				Event: &catalogPb.Event{
					Id: in.GetEventId(), EventDate: in.GetEventDate(), Version: 8,
				},
			}, nil
		},
	}
	resolver := &graph.Resolver{Clients: &grpcclients.Clients{Catalog: client}}

	result, err := resolver.Mutation().UpdateEvent(catalogResolverContext(), "event-id", model.UpdateEventInput{
		Description: &description, EventDate: &eventDate, ExpectedVersion: 7,
	})
	require.NoError(t, err)
	assert.True(t, eventDate.Equal(result.EventDate))
	assert.Equal(t, int64(8), result.Version)
}

func TestCatalogResolvers_Error(t *testing.T) {
	mockAuth := &MockAuthClient{}
	mockCatalog := &MockCatalogClient{
		MockCreateEvent: func(ctx context.Context, in *catalogPb.CreateEventRequest, opts ...grpc.CallOption) (*catalogPb.CreateEventResponse, error) {
			return nil, errors.New("failed to create an event")
		},
		MockUpdateEvent: func(ctx context.Context, in *catalogPb.UpdateEventRequest, opts ...grpc.CallOption) (*catalogPb.UpdateEventResponse, error) {
			return nil, errors.New("failed to update an event")
		},
		MockDeleteEvent: func(ctx context.Context, in *catalogPb.DeleteEventRequest, opts ...grpc.CallOption) (*catalogPb.DeleteEventResponse, error) {
			return nil, errors.New("failed to delete an event")
		},
		MockGetEvent: func(ctx context.Context, in *catalogPb.GetEventRequest, opts ...grpc.CallOption) (*catalogPb.GetEventResponse, error) {
			return &catalogPb.GetEventResponse{
				Event: &catalogPb.Event{
					Id:             in.EventId,
					Title:          "New Event",
					Description:    "Something about event",
					Venue:          "Los-Angeles",
					EventDate:      "2026-05-05T21:00:00Z",
					TotalSeats:     150,
					AvailableSeats: 150,
					PriceMinor:     9999,
				},
			}, nil
		},
	}

	resolver := &graph.Resolver{
		Clients: &grpcclients.Clients{
			Auth:    mockAuth,
			Catalog: mockCatalog,
		},
	}

	title := "New Event"
	description := "Something about event"
	venue := "Los-Angeles"
	eventDate := time.Date(2026, time.May, 5, 21, 00, 00, 00, time.Local)
	totalSeats := int32(150)
	price := int64(9999)
	t.Run("CreateEventError", func(t *testing.T) {
		createEventInput := model.CreateEventInput{
			Title:       title,
			Description: &description,
			Venue:       venue,
			EventDate:   eventDate,
			TotalSeats:  totalSeats,
			PriceMinor:  price,
			Currency:    "RUB",
		}

		result, err := resolver.Mutation().CreateEvent(catalogResolverContext(), createEventInput)
		require.Error(t, err)
		assert.Empty(t, result)

		assert.Contains(t, err.Error(), "failed to create an event")
	})

	t.Run("UpdateEventError", func(t *testing.T) {
		updateEventInput := model.UpdateEventInput{
			Title:           &title,
			Description:     &description,
			Venue:           &venue,
			EventDate:       &eventDate,
			TotalSeats:      &totalSeats,
			PriceMinor:      &price,
			ExpectedVersion: 1,
		}

		result, err := resolver.Mutation().UpdateEvent(catalogResolverContext(), "event-id-1433", updateEventInput)
		require.Error(t, err)
		assert.Empty(t, result)

		assert.Contains(t, err.Error(), "failed to update an event")
	})

	t.Run("DeleteEventError", func(t *testing.T) {
		result, err := resolver.Mutation().DeleteEvent(catalogResolverContext(), "event-id-1433", 1)

		require.Error(t, err)
		assert.False(t, result)

		assert.Contains(t, err.Error(), "failed to delete an event")
	})
}

func TestGetEventResolvers_Success(t *testing.T) {
	mockAuth := &MockAuthClient{}
	mockCatalog := &MockCatalogClient{
		MockGetEvent: func(ctx context.Context, in *catalogPb.GetEventRequest, opts ...grpc.CallOption) (*catalogPb.GetEventResponse, error) {
			assert.Equal(t, "event-id-1566", in.EventId)

			return &catalogPb.GetEventResponse{
				Event: &catalogPb.Event{
					Id:             in.EventId,
					Title:          "Event1234",
					Description:    "something...",
					Venue:          "London",
					EventDate:      "2025-12-31T20:00:00Z",
					TotalSeats:     1000,
					AvailableSeats: 670,
					PriceMinor:     41212,
				},
			}, nil
		},
	}

	resolver := &graph.Resolver{
		Clients: &grpcclients.Clients{
			Auth:    mockAuth,
			Catalog: mockCatalog,
		},
	}

	description := "something..."
	result, err := resolver.Query().GetEvent(context.Background(), "event-id-1566")
	require.NoError(t, err)
	assert.Equal(t, "event-id-1566", result.ID)
	assert.Equal(t, "Event1234", result.Title)
	assert.Equal(t, &description, result.Description)
	assert.Equal(t, "London", result.Venue)
	assert.Equal(t, time.Date(2025, 12, 31, 20, 00, 00, 00, time.UTC), result.EventDate)
	assert.Equal(t, int32(1000), result.TotalSeats)
	assert.Equal(t, int32(670), result.AvailableSeats)
	assert.Equal(t, int64(41212), result.PriceMinor)
}

func TestGetEventResolvers_Error(t *testing.T) {
	mockAuth := &MockAuthClient{}
	mockCatalog := &MockCatalogClient{
		MockGetEvent: func(ctx context.Context, in *catalogPb.GetEventRequest, opts ...grpc.CallOption) (*catalogPb.GetEventResponse, error) {
			return nil, errors.New("failed to get event")
		},
	}

	resolver := &graph.Resolver{
		Clients: &grpcclients.Clients{
			Auth:    mockAuth,
			Catalog: mockCatalog,
		},
	}

	result, err := resolver.Query().GetEvent(context.Background(), "event-id-1566")
	require.Error(t, err)
	assert.Empty(t, result)

	assert.Contains(t, err.Error(), "failed to get event")
}

func TestListEventsResolvers_Success(t *testing.T) {
	testData := []*catalogPb.Event{
		&catalogPb.Event{
			Id:             "event-id-1",
			Title:          "New awesome event",
			Description:    "Welcome to .....",
			Venue:          "London",
			EventDate:      "2025-10-25T20:00:00Z",
			TotalSeats:     int32(500),
			AvailableSeats: int32(500),
			PriceMinor:     15050,
		},
		&catalogPb.Event{
			Id:             "event-id-2",
			Title:          "Event",
			Description:    "something...",
			Venue:          "Amsterdam",
			EventDate:      "2025-12-31T20:00:00Z",
			TotalSeats:     int32(1567),
			AvailableSeats: int32(1200),
			PriceMinor:     9999,
		},
	}
	mockAuth := &MockAuthClient{}
	mockCatalog := &MockCatalogClient{
		MockListEvents: func(ctx context.Context, in *catalogPb.ListEventsRequest, opts ...grpc.CallOption) (*catalogPb.ListEventsResponse, error) {
			assert.Equal(t, int32(1), in.Page)
			assert.Equal(t, int32(20), in.PageSize)
			assert.Equal(t, "event", in.SearchQuery)

			return &catalogPb.ListEventsResponse{
				Events:     testData,
				TotalCount: 2,
				Page:       in.Page,
				PageSize:   in.PageSize,
			}, nil
		},
	}

	resolver := &graph.Resolver{
		Clients: &grpcclients.Clients{
			Auth:    mockAuth,
			Catalog: mockCatalog,
		},
	}

	searchQuery := "event"
	input := model.ListEventsInput{
		Page:        1,
		PageSize:    20,
		SearchQuery: &searchQuery,
	}

	result, err := resolver.Query().ListEvents(context.Background(), input)
	require.NoError(t, err)

	assert.Equal(t, int32(2), result.TotalCount)
	assert.Equal(t, int32(1), result.Page)
	assert.Equal(t, int32(20), result.PageSize)
	for i, event := range result.Events {
		parseDate, _ := time.Parse(time.RFC3339, testData[i].EventDate)
		assert.Equal(t, testData[i].Id, event.ID)
		assert.Equal(t, testData[i].Title, event.Title)
		assert.Equal(t, &testData[i].Description, event.Description)
		assert.Equal(t, testData[i].Venue, event.Venue)
		assert.Equal(t, parseDate, event.EventDate)
		assert.Equal(t, testData[i].TotalSeats, event.TotalSeats)
		assert.Equal(t, testData[i].AvailableSeats, event.AvailableSeats)
		assert.Equal(t, testData[i].PriceMinor, event.PriceMinor)
	}
}

func TestListEventsResolvers_Error(t *testing.T) {
	mockAuth := &MockAuthClient{}
	mockCatalog := &MockCatalogClient{
		MockListEvents: func(ctx context.Context, in *catalogPb.ListEventsRequest, opts ...grpc.CallOption) (*catalogPb.ListEventsResponse, error) {
			assert.Equal(t, int32(1), in.Page)
			assert.Equal(t, int32(20), in.PageSize)
			assert.Equal(t, "event", in.SearchQuery)

			return nil, errors.New("failed to get list of events")
		},
	}

	resolver := &graph.Resolver{
		Clients: &grpcclients.Clients{
			Auth:    mockAuth,
			Catalog: mockCatalog,
		},
	}

	searchQuery := "event"
	input := model.ListEventsInput{
		Page:        1,
		PageSize:    20,
		SearchQuery: &searchQuery,
	}
	result, err := resolver.Query().ListEvents(context.Background(), input)
	require.Error(t, err)
	assert.Empty(t, result)

	assert.Contains(t, err.Error(), "failed to get list of events")
}

func TestGetEventResolvers_NotFoundError(t *testing.T) {
	mockAuth := &MockAuthClient{}
	mockCatalog := &MockCatalogClient{
		MockGetEvent: func(ctx context.Context, in *catalogPb.GetEventRequest, opts ...grpc.CallOption) (*catalogPb.GetEventResponse, error) {
			return nil, errors.New("event not found")
		},
	}
	resolver := &graph.Resolver{
		Clients: &grpcclients.Clients{
			Auth:    mockAuth,
			Catalog: mockCatalog,
		},
	}
	result, err := resolver.Query().GetEvent(context.Background(), "non-existent-id")
	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, strings.ToLower(err.Error()), "not found")
}

func TestGetMeResolver_Success(t *testing.T) {
	mockAuth := &MockAuthClient{
		MockGetMe: func(ctx context.Context, in *authPb.GetMeRequest, _ ...grpc.CallOption) (*authPb.GetMeResponse, error) {
			require.NotNil(t, in)
			md, ok := metadata.FromOutgoingContext(ctx)
			require.True(t, ok)
			assert.Equal(t, []string{"Bearer test-access-token"}, md.Get("authorization"))
			return &authPb.GetMeResponse{
				UserId: "test-user", Email: "test@example.com", Name: "Test User",
				Role: "user", CreatedAt: "2026-05-05T21:00:00Z",
			}, nil
		},
	}
	resolver := graph.NewResolver(&grpcclients.Clients{Auth: mockAuth})
	ctx := context.WithValue(context.Background(), "accessTokenKey", "test-access-token")

	resp, err := resolver.Query().GetMe(ctx)
	require.NoError(t, err)
	assert.Equal(t, &model.GetMePayload{
		UserID: "test-user", Email: "test@example.com", Name: "Test User",
		Role: "user", CreatedAt: "2026-05-05T21:00:00Z",
	}, resp)
}

func TestGetMeResolver_Error(t *testing.T) {
	t.Run("MissingAccessToken", func(t *testing.T) {
		resolver := graph.NewResolver(&grpcclients.Clients{Auth: &MockAuthClient{}})
		resp, err := resolver.Query().GetMe(context.Background())
		require.Error(t, err)
		assert.Nil(t, resp)
		var gqlErr *gqlerror.Error
		require.ErrorAs(t, err, &gqlErr)
		assert.Equal(t, "UNAUTHORIZED", gqlErr.Extensions["code"])
		assert.Equal(t, "authentication required", gqlErr.Message)
	})

	t.Run("AuthRejectsToken", func(t *testing.T) {
		mockAuth := &MockAuthClient{
			MockGetMe: func(context.Context, *authPb.GetMeRequest, ...grpc.CallOption) (*authPb.GetMeResponse, error) {
				return nil, status.Error(codes.Unauthenticated, "invalid access token")
			},
		}
		resolver := graph.NewResolver(&grpcclients.Clients{Auth: mockAuth})
		ctx := context.WithValue(context.Background(), "accessTokenKey", "test-access-token")

		resp, err := resolver.Query().GetMe(ctx)
		require.Error(t, err)
		assert.Nil(t, resp)
		var gqlErr *gqlerror.Error
		require.ErrorAs(t, err, &gqlErr)
		assert.Equal(t, "UNAUTHORIZED", gqlErr.Extensions["code"])
		assert.Equal(t, "invalid access token", gqlErr.Message)
	})
}
