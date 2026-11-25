package tests

import (
	"context"
	"database/sql"
	"eventix/pkg/helpers"
	"eventix/proto/catalog/pb"
	"eventix/services/catalog/internal/catalogctx"
	"eventix/services/catalog/internal/repository"
	catalog "eventix/services/catalog/internal/service"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func catalogActor(t *testing.T, db *sql.DB, role string) (context.Context, uuid.UUID) {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec("INSERT INTO users (id, email, name, role, password_hash) VALUES ($1, $2, 'Catalog test', $3, $4)", id, id.String()+"@example.test", role, []byte("unused"))
	require.NoError(t, err)
	return catalogctx.WithUserIDAndRole(context.Background(), id, role), id
}

func TestCatalog_ConcurrentUpdatesAndStaleDelete(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "catalog")
	defer env.Cleanup(t)

	manager := catalog.NewCatalogManager(repository.NewPostgresStorage(env.DB), slog.New(slog.DiscardHandler))
	owner, _ := catalogActor(t, env.DB, "organizer")
	other, _ := catalogActor(t, env.DB, "organizer")
	admin, _ := catalogActor(t, env.DB, "admin")
	created, err := manager.CreateEvent(owner, &pb.CreateEventRequest{
		Title: "Concurrent updates", Venue: "Hall",
		EventDate:  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		TotalSeats: 100, PriceMinor: 15050, Currency: "RUB",
	})
	require.NoError(t, err)
	event := created.GetEvent()
	require.Equal(t, int64(1), event.GetVersion())

	update := func(title string, version int64) *pb.UpdateEventRequest {
		return &pb.UpdateEventRequest{
			EventId: event.GetId(), ExpectedVersion: version, Title: title,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
		}
	}
	remove := func(version int64) *pb.DeleteEventRequest {
		return &pb.DeleteEventRequest{EventId: event.GetId(), ExpectedVersion: version}
	}

	_, err = manager.UpdateEvent(other, update("Other organizer", 1))
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = manager.DeleteEvent(other, remove(1))
	require.Equal(t, codes.NotFound, status.Code(err))

	type result struct {
		response *pb.UpdateEventResponse
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	go func() {
		<-start
		response, err := manager.UpdateEvent(owner, update("Organizer's edit", 1))
		results <- result{response: response, err: err}
	}()
	go func() {
		<-start
		response, err := manager.UpdateEvent(admin, update("Admin's edit", 1))
		results <- result{response: response, err: err}
	}()
	close(start)

	first, second := <-results, <-results
	if first.err != nil {
		first, second = second, first
	}
	require.NoError(t, first.err)
	require.Equal(t, codes.Aborted, status.Code(second.err))
	require.Equal(t, int64(2), first.response.GetEvent().GetVersion())

	deleted, err := manager.DeleteEvent(owner, remove(1))
	require.Equal(t, codes.Aborted, status.Code(err))
	assert.Nil(t, deleted)

	current, err := manager.GetEvent(context.Background(), &pb.GetEventRequest{EventId: event.GetId()})
	require.NoError(t, err)
	assert.Equal(t, first.response.GetEvent().GetTitle(), current.GetEvent().GetTitle())
	assert.Equal(t, int64(2), current.GetEvent().GetVersion())
	assert.Equal(t, int64(15050), current.GetEvent().GetPriceMinor())
	assert.Equal(t, "RUB", current.GetEvent().GetCurrency())

	deleted, err = manager.DeleteEvent(owner, remove(2))
	require.NoError(t, err)
	assert.True(t, deleted.GetSuccess())
}

func TestCatalog_ResizeCapacityPreservesOccupiedSeats(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "catalog")
	defer env.Cleanup(t)

	manager := catalog.NewCatalogManager(repository.NewPostgresStorage(env.DB), slog.New(slog.DiscardHandler))
	ctx, _ := catalogActor(t, env.DB, "organizer")
	created, err := manager.CreateEvent(ctx, &pb.CreateEventRequest{
		Title: "Capacity", Venue: "Hall",
		EventDate:  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		TotalSeats: 100, PriceMinor: 20000, Currency: "RUB",
	})
	require.NoError(t, err)
	eventID := created.GetEvent().GetId()

	// Simulate 30 occupied seats while Booking is still under development.
	_, err = env.DB.Exec("UPDATE events SET available_seats = 70 WHERE id = $1", eventID)
	require.NoError(t, err)

	resize := func(seats int32, version int64) (*pb.UpdateEventResponse, error) {
		return manager.UpdateEvent(ctx, &pb.UpdateEventRequest{
			EventId: eventID, ExpectedVersion: version, TotalSeats: seats,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"total_seats"}},
		})
	}

	_, err = resize(29, 1)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	resp, err := resize(50, 1)
	require.NoError(t, err)
	assert.Equal(t, int32(50), resp.GetEvent().GetTotalSeats())
	assert.Equal(t, int32(20), resp.GetEvent().GetAvailableSeats())
	assert.Equal(t, int64(2), resp.GetEvent().GetVersion())
}

func TestGRPCServiceCatalog(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "catalog")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	catalogManager := catalog.NewCatalogManager(repo, logger)

	ctx, _ := catalogActor(t, env.DB, "admin")

	var eventID1 string
	t.Run("CreateEvent", func(t *testing.T) {
		req := pb.CreateEventRequest{
			Title:       "Cool event",
			Description: "Wonderful event near you",
			Venue:       "Delaware",
			EventDate:   "2027-06-15T21:00:00.000Z",
			TotalSeats:  150,
			PriceMinor:  9999,
			Currency:    "RUB",
		}

		resp, err := catalogManager.CreateEvent(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Event.Id)
		eventID1 = resp.Event.Id

		assert.Equal(t, "Cool event", resp.Event.Title)
		assert.Equal(t, "Wonderful event near you", resp.Event.Description)
		assert.Equal(t, "Delaware", resp.Event.Venue)
		assert.Equal(t, "2027-06-15T21:00:00Z", resp.Event.EventDate)
		assert.Equal(t, int32(150), resp.Event.TotalSeats)
		assert.Equal(t, int64(9999), resp.Event.PriceMinor)
	})

	t.Run("GetEvent", func(t *testing.T) {
		req := pb.GetEventRequest{
			EventId: eventID1,
		}

		resp, err := catalogManager.GetEvent(ctx, &req)
		require.NoError(t, err)

		assert.Equal(t, eventID1, resp.Event.Id)
		assert.Equal(t, "Cool event", resp.Event.Title)
		assert.Equal(t, "Wonderful event near you", resp.Event.Description)
		assert.Equal(t, "Delaware", resp.Event.Venue)
		assert.Equal(t, "2027-06-15T21:00:00Z", resp.Event.EventDate)
		assert.Equal(t, int32(150), resp.Event.TotalSeats)
		assert.Equal(t, int64(9999), resp.Event.PriceMinor)
	})

	t.Run("UpdateEvent", func(t *testing.T) {
		req := pb.UpdateEventRequest{
			ExpectedVersion: 1,
			UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price_minor", "currency"}},
			EventId:         eventID1,
			Title:           "SUPER AWESOME Cool event",
			Description:     "Wonderful event near you",
			Venue:           "Delaware",
			EventDate:       "2027-06-15T21:00:00.000Z",
			TotalSeats:      250,
			PriceMinor:      9999,
			Currency:        "RUB",
		}

		resp, err := catalogManager.UpdateEvent(ctx, &req)
		require.NoError(t, err)

		assert.Equal(t, eventID1, resp.Event.Id)
		assert.Equal(t, "SUPER AWESOME Cool event", resp.Event.Title)
		assert.Equal(t, "Wonderful event near you", resp.Event.Description)
		assert.Equal(t, "Delaware", resp.Event.Venue)
		assert.Equal(t, "2027-06-15T21:00:00Z", resp.Event.EventDate)
		assert.Equal(t, int32(250), resp.Event.TotalSeats)
		assert.Equal(t, int64(9999), resp.Event.PriceMinor)
	})

	var eventID2 string
	t.Run("CreateEvent", func(t *testing.T) {
		req := pb.CreateEventRequest{
			Title:       "New event",
			Description: "Test description",
			Venue:       "Los Angeles",
			EventDate:   "2027-11-22T17:00:00.000Z",
			TotalSeats:  2000,
			PriceMinor:  12000,
			Currency:    "RUB",
		}

		resp, err := catalogManager.CreateEvent(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		eventID2 = resp.Event.Id
	})

	t.Run("ListEvents", func(t *testing.T) {
		req := pb.ListEventsRequest{
			Page:        1,
			PageSize:    20,
			SearchQuery: "event",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		assert.Equal(t, 2, len(resp.Events))

		assert.Equal(t, eventID2, resp.Events[0].Id)
		assert.Equal(t, "New event", resp.Events[0].Title)
		assert.Equal(t, "Test description", resp.Events[0].Description)
		assert.Equal(t, "Los Angeles", resp.Events[0].Venue)
		assert.Equal(t, "2027-11-22T17:00:00Z", resp.Events[0].EventDate)
		assert.Equal(t, int32(2000), resp.Events[0].TotalSeats)
		assert.Equal(t, int64(100*(120)), resp.Events[0].PriceMinor)

		assert.Equal(t, eventID1, resp.Events[1].Id)
		assert.Equal(t, "SUPER AWESOME Cool event", resp.Events[1].Title)
		assert.Equal(t, "Wonderful event near you", resp.Events[1].Description)
		assert.Equal(t, "Delaware", resp.Events[1].Venue)
		assert.Equal(t, "2027-06-15T21:00:00Z", resp.Events[1].EventDate)
		assert.Equal(t, int32(250), resp.Events[1].TotalSeats)
		assert.Equal(t, int64(9999), resp.Events[1].PriceMinor)
	})

	t.Run("DeleteEvent", func(t *testing.T) {
		req := pb.DeleteEventRequest{
			ExpectedVersion: 2,
			EventId:         eventID1,
		}

		resp, err := catalogManager.DeleteEvent(ctx, &req)
		require.NoError(t, err)
		assert.Equal(t, true, resp.Success)
	})

	t.Run("GetEventAfterDelete", func(t *testing.T) {
		req := pb.GetEventRequest{
			EventId: eventID1,
		}

		resp, err := catalogManager.GetEvent(ctx, &req)
		require.Error(t, err)
		assert.Empty(t, resp)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
		assert.Equal(t, "event not found", st.Message())
	})

	t.Run("GetEvent2", func(t *testing.T) {
		req := pb.GetEventRequest{
			EventId: eventID2,
		}

		resp, err := catalogManager.GetEvent(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)

		assert.Equal(t, eventID2, resp.Event.Id)
		assert.Equal(t, "New event", resp.Event.Title)
		assert.Equal(t, "Test description", resp.Event.Description)
		assert.Equal(t, "Los Angeles", resp.Event.Venue)
		assert.Equal(t, "2027-11-22T17:00:00Z", resp.Event.EventDate)
		assert.Equal(t, int32(2000), resp.Event.TotalSeats)
		assert.Equal(t, int64(100*(120)), resp.Event.PriceMinor)
	})
}

func TestCatalog_EventNotFoundAndDuplicateKey(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "catalog")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	catalogManager := catalog.NewCatalogManager(repo, logger)

	ctx, _ := catalogActor(t, env.DB, "admin")

	req := pb.CreateEventRequest{
		Title:       "Test Event",
		Description: "",
		Venue:       "Dallas",
		EventDate:   "2027-06-15T21:00:00.000Z",
		TotalSeats:  5000,
		PriceMinor:  2011,
		Currency:    "RUB",
	}

	resp, err := catalogManager.CreateEvent(ctx, &req)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Event.Id)

	assert.Equal(t, "Test Event", resp.Event.Title)
	assert.Equal(t, "", resp.Event.Description)
	assert.Equal(t, "Dallas", resp.Event.Venue)
	assert.Equal(t, "2027-06-15T21:00:00Z", resp.Event.EventDate)
	assert.Equal(t, int32(5000), resp.Event.TotalSeats)
	assert.Equal(t, int64(2011), resp.Event.PriceMinor)

	t.Run("GetNotExistEvent", func(t *testing.T) {
		req := pb.GetEventRequest{
			EventId: uuid.NewString(),
		}

		resp, err := catalogManager.GetEvent(ctx, &req)
		require.Error(t, err)
		assert.Empty(t, resp)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
		assert.Equal(t, "event not found", st.Message())
	})

	t.Run("CreateDuplicateEvent", func(t *testing.T) {
		req := pb.CreateEventRequest{
			Title:       "Test Event",
			Description: "",
			Venue:       "Dallas",
			EventDate:   "2027-06-15T21:00:00.000Z",
			TotalSeats:  5000,
			PriceMinor:  2011,
			Currency:    "RUB",
		}

		resp, err := catalogManager.CreateEvent(ctx, &req)
		require.Error(t, err)
		assert.Empty(t, resp)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.AlreadyExists, st.Code())
		assert.Equal(t, "event title already exists", st.Message())
	})

	t.Run("UpdateNotExistEvent", func(t *testing.T) {
		req := pb.UpdateEventRequest{
			ExpectedVersion: 1,
			UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"title", "description", "venue", "event_date", "total_seats", "price_minor", "currency"}},
			EventId:         uuid.NewString(),
			Title:           "Fodjgss",
			Description:     "",
			Venue:           "Dallas",
			EventDate:       "2027-06-15T21:00:00.000Z",
			TotalSeats:      5000,
			PriceMinor:      2011,
			Currency:        "RUB",
		}

		resp, err := catalogManager.UpdateEvent(ctx, &req)
		require.Error(t, err)
		assert.Empty(t, resp)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
		assert.Equal(t, "event not found", st.Message())
	})

	t.Run("DeleteNotExistEvent", func(t *testing.T) {
		req := pb.DeleteEventRequest{
			ExpectedVersion: 2,
			EventId:         uuid.NewString(),
		}

		resp, err := catalogManager.DeleteEvent(ctx, &req)
		require.Error(t, err)
		assert.Empty(t, resp)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
		assert.Equal(t, "event not found", st.Message())
	})
}

func TestCatalog_PaginationAndSearchQuery(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "catalog")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	catalogManager := catalog.NewCatalogManager(repo, logger)

	ctx, _ := catalogActor(t, env.DB, "admin")

	citys := []string{"London", "Berlin", "Gamburg", "Paris", "Butan", "Hon kong", "Tokyo", "Osaka", "London", "Berlin", "Gamburg", "Paris", "Butan", "Hon kong", "Tokyo", "Osaka", "Hon kong", "Tokyo", "London", "Berlin"}

	var req pb.CreateEventRequest
	for i := 0; i < 20; i++ {
		if i%2 == 0 {
			req = pb.CreateEventRequest{
				Title:       "Concert " + strconv.Itoa(i),
				Description: "Description " + strconv.Itoa(i),
				Venue:       citys[i],
				EventDate:   time.Date(2027, 5, 2-2*i, 21, 0, 0, 0, time.UTC).Format(time.RFC3339),
				TotalSeats:  int32(250 + 50*i),
				PriceMinor:  int64(100 * (20 + 50*i)),
				Currency:    "RUB",
			}
		} else {
			req = pb.CreateEventRequest{
				Title:       "Concert " + strconv.Itoa(i) + " event",
				Description: "Description " + strconv.Itoa(i),
				Venue:       citys[i],
				EventDate:   time.Date(2027, 5, 2-2*i, 21, 0, 0, 0, time.UTC).Format(time.RFC3339),
				TotalSeats:  int32(250 + 50*i),
				PriceMinor:  int64(100 * (20 + 50*i)),
				Currency:    "RUB",
			}
		}

		resp, err := catalogManager.CreateEvent(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
	}

	t.Run("ListOf4Events", func(t *testing.T) {
		req := pb.ListEventsRequest{
			Page:        1,
			PageSize:    4,
			SearchQuery: "",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		assert.Equal(t, 4, len(resp.Events))
		assert.Equal(t, int32(20), resp.TotalCount)

		var title string
		for i := 0; i < 4; i++ {
			assert.NotEmpty(t, resp.Events[i].Id)
			if i%2 == 0 {
				title = "Concert " + strconv.Itoa(i)
			} else {
				title = "Concert " + strconv.Itoa(i) + " event"
			}
			assert.Equal(t, title, resp.Events[i].Title)
			assert.Equal(t, "Description "+strconv.Itoa(i), resp.Events[i].Description)
			assert.Equal(t, citys[i], resp.Events[i].Venue)
			assert.Equal(t, time.Date(2027, 5, 2-2*i, 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[i].EventDate)
			assert.Equal(t, int32(250+50*i), resp.Events[i].TotalSeats)
			assert.Equal(t, int64(100*(20+50*i)), resp.Events[i].PriceMinor)
		}
	})

	t.Run("ListOf2Events", func(t *testing.T) {
		req := pb.ListEventsRequest{
			Page:        1,
			PageSize:    2,
			SearchQuery: "",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		assert.Equal(t, 2, len(resp.Events))
		assert.Equal(t, int32(20), resp.TotalCount)

		assert.NotEmpty(t, resp.Events[0].Id)
		assert.Equal(t, "Concert 0", resp.Events[0].Title)
		assert.Equal(t, "Description 0", resp.Events[0].Description)
		assert.Equal(t, "London", resp.Events[0].Venue)
		assert.Equal(t, time.Date(2027, 5, 2-2*0, 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[0].EventDate)
		assert.Equal(t, int32(250+50*0), resp.Events[0].TotalSeats)
		assert.Equal(t, int64(100*(20+50*0)), resp.Events[0].PriceMinor)

		assert.NotEmpty(t, resp.Events[1].Id)
		assert.Equal(t, "Concert 1 event", resp.Events[1].Title)
		assert.Equal(t, "Description 1", resp.Events[1].Description)
		assert.Equal(t, "Berlin", resp.Events[1].Venue)
		assert.Equal(t, time.Date(2027, 5, 2-2*1, 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[1].EventDate)
		assert.Equal(t, int32(250+50*1), resp.Events[1].TotalSeats)
		assert.Equal(t, int64(100*(20+50*1)), resp.Events[1].PriceMinor)

	})

	t.Run("ListOf5EventsWithSearchQuery_FirstPage", func(t *testing.T) {
		req := pb.ListEventsRequest{
			Page:        1,
			PageSize:    5,
			SearchQuery: "event",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		assert.Equal(t, 5, len(resp.Events))
		assert.Equal(t, int32(10), resp.TotalCount)

		for i := 0; i < 5; i++ {
			assert.NotEmpty(t, resp.Events[i].Id)
			assert.Equal(t, "Concert "+strconv.Itoa(2*i+1)+" event", resp.Events[i].Title)
			assert.Equal(t, "Description "+strconv.Itoa(2*i+1), resp.Events[i].Description)
			assert.Equal(t, citys[2*i+1], resp.Events[i].Venue)
			assert.Equal(t, time.Date(2027, 5, 2-2*(2*i+1), 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[i].EventDate)
			assert.Equal(t, int32(250+50*(2*i+1)), resp.Events[i].TotalSeats)
			assert.Equal(t, int64(100*(20+50*(2*i+1))), resp.Events[i].PriceMinor)
		}
	})

	t.Run("ListOf5EventsWithSearchQuery_SecondPage", func(t *testing.T) {
		req := pb.ListEventsRequest{
			Page:        2,
			PageSize:    5,
			SearchQuery: "event",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		assert.Equal(t, 5, len(resp.Events))
		assert.Equal(t, int32(10), resp.TotalCount)

		for i := 0; i < 5; i++ {
			assert.NotEmpty(t, resp.Events[i].Id)
			assert.Equal(t, "Concert "+strconv.Itoa(10+2*i+1)+" event", resp.Events[i].Title)
			assert.Equal(t, "Description "+strconv.Itoa(10+2*i+1), resp.Events[i].Description)
			assert.Equal(t, citys[10+2*i+1], resp.Events[i].Venue)
			assert.Equal(t, time.Date(2027, 5, 2-2*(10+2*i+1), 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[i].EventDate)
			assert.Equal(t, int32(250+50*(10+2*i+1)), resp.Events[i].TotalSeats)
			assert.Equal(t, int64(100*(20+50*(10+2*i+1))), resp.Events[i].PriceMinor)
		}
	})

	t.Run("ListOf5EventsWithSearchQuery_Third", func(t *testing.T) {
		req := pb.ListEventsRequest{
			Page:        3,
			PageSize:    5,
			SearchQuery: "event",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		assert.Equal(t, 0, len(resp.Events))
		assert.Equal(t, int32(10), resp.TotalCount)
	})

	t.Run("ListOf5EventsWithSearchQuery_2", func(t *testing.T) {
		req := pb.ListEventsRequest{
			Page:        1,
			PageSize:    5,
			SearchQuery: "London",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		assert.Equal(t, 3, len(resp.Events))
		assert.Equal(t, int32(3), resp.TotalCount)

		assert.NotEmpty(t, resp.Events[0].Id)
		assert.Equal(t, "Concert 0", resp.Events[0].Title)
		assert.Equal(t, "Description 0", resp.Events[0].Description)
		assert.Equal(t, citys[0], resp.Events[0].Venue)
		assert.Equal(t, time.Date(2027, 5, 2-2*0, 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[0].EventDate)
		assert.Equal(t, int32(250+50*0), resp.Events[0].TotalSeats)
		assert.Equal(t, int64(100*(20+50*0)), resp.Events[0].PriceMinor)

		assert.NotEmpty(t, resp.Events[1].Id)
		assert.Equal(t, "Concert 8", resp.Events[1].Title)
		assert.Equal(t, "Description 8", resp.Events[1].Description)
		assert.Equal(t, citys[8], resp.Events[1].Venue)
		assert.Equal(t, time.Date(2027, 5, 2-2*8, 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[1].EventDate)
		assert.Equal(t, int32(250+50*8), resp.Events[1].TotalSeats)
		assert.Equal(t, int64(100*(20+50*8)), resp.Events[1].PriceMinor)

		assert.NotEmpty(t, resp.Events[2].Id)
		assert.Equal(t, "Concert 18", resp.Events[2].Title)
		assert.Equal(t, "Description 18", resp.Events[2].Description)
		assert.Equal(t, citys[18], resp.Events[2].Venue)
		assert.Equal(t, time.Date(2027, 5, 2-2*18, 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[2].EventDate)
		assert.Equal(t, int32(250+50*18), resp.Events[2].TotalSeats)
		assert.Equal(t, int64(100*(20+50*18)), resp.Events[2].PriceMinor)

	})

	req = pb.CreateEventRequest{
		Title:       "Cool event 100%",
		Description: "Wonderful event near you",
		Venue:       "Delaware",
		EventDate:   "2027-06-15T21:00:00.000Z",
		TotalSeats:  150,
		PriceMinor:  9999,
		Currency:    "RUB",
	}

	resp, err := catalogManager.CreateEvent(ctx, &req)
	require.NoError(t, err)
	assert.NotEmpty(t, resp)

	t.Run("ChangeSpecialSymbolsInSearchQuery", func(t *testing.T) {

		req := pb.ListEventsRequest{
			Page:        1,
			PageSize:    20,
			SearchQuery: "100%",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		assert.Equal(t, 1, len(resp.Events))

		assert.NotEmpty(t, resp.Events[0].Id)
		assert.Equal(t, "Cool event 100%", resp.Events[0].Title)
		assert.Equal(t, "Wonderful event near you", resp.Events[0].Description)
		assert.Equal(t, "Delaware", resp.Events[0].Venue)
		assert.Equal(t, "2027-06-15T21:00:00Z", resp.Events[0].EventDate)
		assert.Equal(t, int32(150), resp.Events[0].TotalSeats)
		assert.Equal(t, int64(9999), resp.Events[0].PriceMinor)
	})

	t.Run("SQLIjectionInSeqrchQuery", func(t *testing.T) {

		req := pb.ListEventsRequest{
			Page:        1,
			PageSize:    20,
			SearchQuery: "SELECT * FROM events;",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)
		assert.Empty(t, resp.Events)
	})

	t.Run("SQLInjectionAttemptDoesNotBreakTable", func(t *testing.T) {
		req := pb.ListEventsRequest{
			Page:        1,
			PageSize:    20,
			SearchQuery: "'; DROP TABLE events; --",
		}

		resp, err := catalogManager.ListEvents(ctx, &req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		var count int
		err = env.DB.QueryRow(`SELECT COUNT(*) FROM events;`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 21, count)
	})
}
