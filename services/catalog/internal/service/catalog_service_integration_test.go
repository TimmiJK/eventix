package catalog

import (
	"context"
	"database/sql"
	"eventix/pkg/postgres"
	"eventix/proto/catalog/pb"
	"eventix/services/catalog/internal/repository"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ConnectTestDb() *sql.DB {
	if err := godotenv.Load("../../../../.env"); err != nil {
		log.Println(" .env file not found, using environment variables")
	}

	dbHost := os.Getenv("DB_HOST_TEST")
	dbPort := os.Getenv("DB_PORT_TEST")
	dbUser := os.Getenv("DB_USER_TEST")
	dbPass := os.Getenv("DB_PASSWORD_TEST")
	dbName := os.Getenv("DB_NAME_TEST")

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", dbHost, dbPort, dbUser, dbPass, dbName)

	db, err := postgres.NewPostgresDB(dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to TestPostgreSQL")
	return db
}

func TestGRPCServiceCatalog(t *testing.T) {
	db := ConnectTestDb()
	defer db.Close()
	defer func() { db.Exec("DELETE FROM events;") }()

	repo := repository.NewPostgresStorage(db)
	logger := slog.New(slog.DiscardHandler)
	catalogManager := NewCatalogManager(repo, logger)

	ctx := context.Background()

	var eventID1 string
	t.Run("CreateEvent", func(t *testing.T) {
		req := pb.CreateEventRequest{
			Title:       "Cool event",
			Description: "Wonderful event near you",
			Venue:       "Delaware",
			EventDate:   "2027-06-15T21:00:00.000Z",
			TotalSeats:  150,
			Price:       99.99,
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
		assert.Equal(t, 99.99, resp.Event.Price)
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
		assert.Equal(t, 99.99, resp.Event.Price)
	})

	t.Run("UpdateEvent", func(t *testing.T) {
		req := pb.UpdateEventRequest{
			EventId:     eventID1,
			Title:       "SUPER AWESOME Cool event",
			Description: "Wonderful event near you",
			Venue:       "Delaware",
			EventDate:   "2027-06-15T21:00:00.000Z",
			TotalSeats:  250,
			Price:       99.99,
		}

		resp, err := catalogManager.UpdateEvent(ctx, &req)
		require.NoError(t, err)

		assert.Equal(t, eventID1, resp.Event.Id)
		assert.Equal(t, "SUPER AWESOME Cool event", resp.Event.Title)
		assert.Equal(t, "Wonderful event near you", resp.Event.Description)
		assert.Equal(t, "Delaware", resp.Event.Venue)
		assert.Equal(t, "2027-06-15T21:00:00Z", resp.Event.EventDate)
		assert.Equal(t, int32(250), resp.Event.TotalSeats)
		assert.Equal(t, 99.99, resp.Event.Price)
	})

	var eventID2 string
	t.Run("CreateEvent", func(t *testing.T) {
		req := pb.CreateEventRequest{
			Title:       "New event",
			Description: "Test description",
			Venue:       "Los Angeles",
			EventDate:   "2027-11-22T17:00:00.000Z",
			TotalSeats:  2000,
			Price:       120,
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
		assert.Equal(t, float64(120), resp.Events[0].Price)

		assert.Equal(t, eventID1, resp.Events[1].Id)
		assert.Equal(t, "SUPER AWESOME Cool event", resp.Events[1].Title)
		assert.Equal(t, "Wonderful event near you", resp.Events[1].Description)
		assert.Equal(t, "Delaware", resp.Events[1].Venue)
		assert.Equal(t, "2027-06-15T21:00:00Z", resp.Events[1].EventDate)
		assert.Equal(t, int32(250), resp.Events[1].TotalSeats)
		assert.Equal(t, 99.99, resp.Events[1].Price)
	})

	t.Run("DeleteEvent", func(t *testing.T) {
		req := pb.DeleteEventRequest{
			EventId: eventID1,
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
		assert.Equal(t, float64(120), resp.Event.Price)
	})
}

func TestCatalog_EventNotFoundAndDuplicateKey(t *testing.T) {
	db := ConnectTestDb()
	defer db.Close()
	defer func() { db.Exec("DELETE FROM events;") }()

	repo := repository.NewPostgresStorage(db)
	logger := slog.New(slog.DiscardHandler)
	catalogManager := NewCatalogManager(repo, logger)

	ctx := context.Background()

	req := pb.CreateEventRequest{
		Title:       "Test Event",
		Description: "",
		Venue:       "Dallas",
		EventDate:   "2027-06-15T21:00:00.000Z",
		TotalSeats:  5000,
		Price:       20.11,
	}

	resp, err := catalogManager.CreateEvent(ctx, &req)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Event.Id)

	assert.Equal(t, "Test Event", resp.Event.Title)
	assert.Equal(t, "", resp.Event.Description)
	assert.Equal(t, "Dallas", resp.Event.Venue)
	assert.Equal(t, "2027-06-15T21:00:00Z", resp.Event.EventDate)
	assert.Equal(t, int32(5000), resp.Event.TotalSeats)
	assert.Equal(t, 20.11, resp.Event.Price)

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
			Price:       20.11,
		}

		resp, err := catalogManager.CreateEvent(ctx, &req)
		require.Error(t, err)
		assert.Empty(t, resp)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.AlreadyExists, st.Code())
		assert.Equal(t, "event already created", st.Message())
	})

	t.Run("UpdateNotExistEvent", func(t *testing.T) {
		req := pb.UpdateEventRequest{
			EventId:     uuid.NewString(),
			Title:       "Fodjgss",
			Description: "",
			Venue:       "Dallas",
			EventDate:   "2027-06-15T21:00:00.000Z",
			TotalSeats:  5000,
			Price:       20.11,
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
			EventId: uuid.NewString(),
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
	db := ConnectTestDb()
	defer db.Close()
	_, err := db.Exec("DELETE FROM events;")
	require.NoError(t, err)
	defer func() { db.Exec("DELETE FROM events;") }()

	repo := repository.NewPostgresStorage(db)
	logger := slog.New(slog.DiscardHandler)
	catalogManager := NewCatalogManager(repo, logger)

	ctx := context.Background()

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
				Price:       float64(20 + 50*i),
			}
		} else {
			req = pb.CreateEventRequest{
				Title:       "Concert " + strconv.Itoa(i) + " event",
				Description: "Description " + strconv.Itoa(i),
				Venue:       citys[i],
				EventDate:   time.Date(2027, 5, 2-2*i, 21, 0, 0, 0, time.UTC).Format(time.RFC3339),
				TotalSeats:  int32(250 + 50*i),
				Price:       float64(20 + 50*i),
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
			assert.Equal(t, float64(20+50*i), resp.Events[i].Price)
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
		assert.Equal(t, float64(20+50*0), resp.Events[0].Price)

		assert.NotEmpty(t, resp.Events[1].Id)
		assert.Equal(t, "Concert 1 event", resp.Events[1].Title)
		assert.Equal(t, "Description 1", resp.Events[1].Description)
		assert.Equal(t, "Berlin", resp.Events[1].Venue)
		assert.Equal(t, time.Date(2027, 5, 2-2*1, 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[1].EventDate)
		assert.Equal(t, int32(250+50*1), resp.Events[1].TotalSeats)
		assert.Equal(t, float64(20+50*1), resp.Events[1].Price)

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
			assert.Equal(t, float64(20+50*(2*i+1)), resp.Events[i].Price)
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
			assert.Equal(t, float64(20+50*(10+2*i+1)), resp.Events[i].Price)
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
		assert.Equal(t, float64(20+50*0), resp.Events[0].Price)

		assert.NotEmpty(t, resp.Events[1].Id)
		assert.Equal(t, "Concert 8", resp.Events[1].Title)
		assert.Equal(t, "Description 8", resp.Events[1].Description)
		assert.Equal(t, citys[8], resp.Events[1].Venue)
		assert.Equal(t, time.Date(2027, 5, 2-2*8, 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[1].EventDate)
		assert.Equal(t, int32(250+50*8), resp.Events[1].TotalSeats)
		assert.Equal(t, float64(20+50*8), resp.Events[1].Price)

		assert.NotEmpty(t, resp.Events[2].Id)
		assert.Equal(t, "Concert 18", resp.Events[2].Title)
		assert.Equal(t, "Description 18", resp.Events[2].Description)
		assert.Equal(t, citys[18], resp.Events[2].Venue)
		assert.Equal(t, time.Date(2027, 5, 2-2*18, 21, 0, 0, 0, time.UTC).Format(time.RFC3339), resp.Events[2].EventDate)
		assert.Equal(t, int32(250+50*18), resp.Events[2].TotalSeats)
		assert.Equal(t, float64(20+50*18), resp.Events[2].Price)

	})

	req = pb.CreateEventRequest{
		Title:       "Cool event 100%",
		Description: "Wonderful event near you",
		Venue:       "Delaware",
		EventDate:   "2027-06-15T21:00:00.000Z",
		TotalSeats:  150,
		Price:       99.99,
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
		assert.Equal(t, 99.99, resp.Events[0].Price)
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
		err = db.QueryRow(`SELECT COUNT(*) FROM events;`).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 21, count)
	})
}
