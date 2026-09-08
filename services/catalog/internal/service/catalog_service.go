package catalog

import (
	"context"
	"database/sql"
	"errors"
	"eventix/proto/catalog/pb"
	"eventix/services/catalog/internal/models"
	"eventix/services/catalog/internal/repository"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CatalogManager struct {
	repo   repository.Repository
	logger *slog.Logger
	pb.UnimplementedCatalogServiceServer
}

func NewCatalogManager(repo repository.Repository, logger *slog.Logger) *CatalogManager {
	return &CatalogManager{
		repo:   repo,
		logger: logger,
	}
}

func validatePagination(page int32, pageSize int32) (int32, int32, error) {
	if page <= 0 {
		page = 1
	}

	if pageSize <= 0 {
		pageSize = 20
	}

	if pageSize > 100 {
		return 0, 0, status.Error(
			codes.InvalidArgument,
			"page_size cannot be greater than 100",
		)
	}

	return page, pageSize, nil
}

func (m *CatalogManager) GetEvent(ctx context.Context, req *pb.GetEventRequest) (*pb.GetEventResponse, error) {
	m.logger.Info("GetEvent attempt", "event_id", req.EventId)

	id, err := uuid.Parse(req.EventId)
	if err != nil {
		m.logger.Warn("GetEvent failed: invalid event ID format", "event_id", req.EventId, "error", err)
		return nil, status.Error(codes.InvalidArgument, "invalid event id format")
	}

	event, err := m.repo.GetEventByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			m.logger.Warn("GetEvent failed: event not found", "event_id", req.EventId)
			return nil, status.Error(codes.NotFound, "event not found")
		}

		m.logger.Error("GetEvent failed: database error", "event_id", req.EventId, "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	pbEvent := &pb.Event{
		Id:             event.ID.String(),
		Title:          event.Title,
		Description:    event.Description,
		Venue:          event.Venue,
		EventDate:      event.EventDate.Format(time.RFC3339),
		TotalSeats:     event.TotalSeats,
		AvailableSeats: event.AvailableSeats,
		Price:          event.Price,
	}

	m.logger.Info("GetEvent successful", "event_id", req.EventId)
	return &pb.GetEventResponse{Event: pbEvent}, nil
}

func (m *CatalogManager) ListEvents(ctx context.Context, req *pb.ListEventsRequest) (*pb.ListEventsResponse, error) {
	m.logger.Info("ListEvents attempt", "page", req.Page, "page_size", req.PageSize, "search_query", req.SearchQuery)

	page, pageSize, err := validatePagination(req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}

	events, totalCount, err := m.repo.GetEvents(ctx, page, pageSize, req.SearchQuery)
	if err != nil {
		m.logger.Error("ListEvents failed: database error", "page", req.Page, "page_size", req.PageSize, "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	pbEvents := make([]*pb.Event, len(events))
	for i, val := range events {
		pbEvents[i] = &pb.Event{
			Id:             val.ID.String(),
			Title:          val.Title,
			Description:    val.Description,
			Venue:          val.Venue,
			EventDate:      val.EventDate.Format(time.RFC3339),
			TotalSeats:     val.TotalSeats,
			AvailableSeats: val.AvailableSeats,
			Price:          val.Price,
		}
	}

	m.logger.Info("ListEvents successful", "page", req.Page, "page_size", req.PageSize, "returned_count", len(events), "total_count", totalCount)
	return &pb.ListEventsResponse{
		Events:     pbEvents,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
	}, nil
}

func (m *CatalogManager) CreateEvent(ctx context.Context, req *pb.CreateEventRequest) (*pb.CreateEventResponse, error) {
	m.logger.Info("CreateEvent attempt", "title", req.Title, "description", req.Description)

	eventDate, err := time.Parse(time.RFC3339, req.EventDate)
	if err != nil {
		m.logger.Warn("CreateEvent failed: invalid event date format", "event_date", req.EventDate, "error", err)
		return nil, status.Error(codes.InvalidArgument, "invalid event date format")
	}

	if strings.TrimSpace(req.Title) == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	if len(req.Title) > 200 {
		return nil, status.Error(codes.InvalidArgument, "title is too long")
	}
	if req.Price <= 0 {
		m.logger.Warn("CreateEvent failed: price cannot be negative", "title", req.Title, "description", req.Description)
		return nil, status.Error(codes.InvalidArgument, "price cannot be negative")
	}
	if req.TotalSeats < 0 {
		m.logger.Warn("CreateEvent failed: totalSeats cannot be negative", "title", req.Title, "description", req.Description)
		return nil, status.Error(codes.InvalidArgument, "totalSeats cannot be negative")
	}

	if eventDate.Before(time.Now().Add(-1 * time.Hour)) {
		return nil, status.Error(codes.InvalidArgument, "eventDate cannot be in the past")
	}

	event := &models.Event{
		Title:          req.Title,
		Description:    req.Description,
		Venue:          req.Venue,
		EventDate:      eventDate,
		TotalSeats:     req.TotalSeats,
		AvailableSeats: req.TotalSeats,
		Price:          req.Price,
	}

	createdEvent, err := m.repo.CreateEvent(ctx, event)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			m.logger.Warn("CreateEvent failed: event already exists", "title", req.Title, "description", req.Description)
			return nil, status.Error(codes.AlreadyExists, "event already created")
		}
		m.logger.Error("CreateEvent failed: database error", "title", req.Title, "description", req.Description, "error", err)
		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	pbEvent := &pb.Event{
		Id:             createdEvent.ID.String(),
		Title:          createdEvent.Title,
		Description:    createdEvent.Description,
		Venue:          createdEvent.Venue,
		EventDate:      createdEvent.EventDate.Format(time.RFC3339),
		TotalSeats:     createdEvent.TotalSeats,
		AvailableSeats: createdEvent.AvailableSeats,
		Price:          createdEvent.Price,
	}

	m.logger.Info("CreateEvent successful", "event_id", createdEvent.ID.String())
	return &pb.CreateEventResponse{Event: pbEvent}, nil
}

func (m *CatalogManager) UpdateEvent(ctx context.Context, req *pb.UpdateEventRequest) (*pb.UpdateEventResponse, error) {
	m.logger.Info("UpdateEvent attempt", "event_id", req.EventId)

	uuidID, err := uuid.Parse(req.EventId)
	if err != nil {
		m.logger.Warn("UpdateEvent failed: invalid event ID format", "event_id", req.EventId, "error", err)
		return nil, status.Error(codes.InvalidArgument, "invalid event id format")
	}

	existingEvent, err := m.repo.GetEventByID(ctx, uuidID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			m.logger.Warn("UpdateEvent failed: event not found", "event_id", req.EventId)
			return nil, status.Error(codes.NotFound, "event not found")
		}
		m.logger.Error("UpdateEvent failed: database error", "event_id", req.EventId, "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	if req.UpdateMask != nil {
		for _, path := range req.UpdateMask.Paths {
			switch path {
			case "title":
				if strings.TrimSpace(req.Title) == "" {
					return nil, status.Error(codes.InvalidArgument, "title is required")
				}
				if len(req.Title) > 200 {
					return nil, status.Error(codes.InvalidArgument, "title is too long")
				}

				existingEvent.Title = req.Title
			case "description":
				existingEvent.Description = req.Description
			case "venue":
				existingEvent.Venue = req.Venue
			case "event_date":
				if req.EventDate == "" {
					return nil, status.Error(codes.InvalidArgument, "event_date is required")
				}

				eventDate, err := time.Parse(time.RFC3339, req.EventDate)
				if err != nil {
					m.logger.Warn("UpdateEvent failed: invalid event date format", "event_date", req.EventDate, "error", err)
					return nil, status.Error(codes.InvalidArgument, "invalid event date format")
				}
				if eventDate.Before(time.Now().Add(-1 * time.Hour)) {
					return nil, status.Error(codes.InvalidArgument, "eventDate cannot be in the past")
				}

				existingEvent.EventDate = eventDate
			case "total_seats":
				if req.TotalSeats < 0 {
					return nil, status.Error(codes.InvalidArgument, "totalSeats cannot be negative")
				}

				existingEvent.TotalSeats = req.TotalSeats
			case "price":
				if req.Price <= 0 {
					return nil, status.Error(codes.InvalidArgument, "price cannot be negative")
				}

				existingEvent.Price = req.Price
			default:
				m.logger.Warn("UpdateEvent failed: unknown field in mask", "field", path)
				return nil, status.Errorf(codes.InvalidArgument, "unknown field: %s", path)
			}
		}
	} else {
		if req.EventDate == "" {
			return nil, status.Error(codes.InvalidArgument, "event_date is required")
		}

		eventDate, err := time.Parse(time.RFC3339, req.EventDate)
		if err != nil {
			m.logger.Warn("UpdateEvent failed: invalid event date format", "event_date", req.EventDate, "error", err)
			return nil, status.Error(codes.InvalidArgument, "invalid event date format")
		}

		if strings.TrimSpace(req.Title) == "" {
			return nil, status.Error(codes.InvalidArgument, "title is required")
		}
		if len(req.Title) > 200 {
			return nil, status.Error(codes.InvalidArgument, "title is too long")
		}
		if req.Price <= 0 {
			m.logger.Warn("UpdateEvent failed: price cannot be negative", "event_id", req.EventId)
			return nil, status.Error(codes.InvalidArgument, "price cannot be negative")
		}
		if req.TotalSeats < 0 {
			m.logger.Warn("UpdateEvent failed: totalSeats cannot be negative", "event_id", req.EventId)
			return nil, status.Error(codes.InvalidArgument, "totalSeats cannot be negative")
		}
		if eventDate.Before(time.Now().Add(-1 * time.Hour)) {
			return nil, status.Error(codes.InvalidArgument, "eventDate cannot be in the past")
		}

		existingEvent.Title = req.Title
		existingEvent.Description = req.Description
		existingEvent.Venue = req.Venue
		existingEvent.EventDate = eventDate
		existingEvent.TotalSeats = req.TotalSeats
		existingEvent.Price = req.Price
	}

	updatedEvent, err := m.repo.UpdateEvent(ctx, &existingEvent)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			m.logger.Warn("UpdateEvent failed: event not found", "event_id", req.EventId)
			return nil, status.Error(codes.NotFound, "event not found")
		}
		m.logger.Error("UpdateEvent failed: database error", "event_id", req.EventId, "error", err)
		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	pbEvent := &pb.Event{
		Id:             updatedEvent.ID.String(),
		Title:          updatedEvent.Title,
		Description:    updatedEvent.Description,
		Venue:          updatedEvent.Venue,
		EventDate:      updatedEvent.EventDate.Format(time.RFC3339),
		TotalSeats:     updatedEvent.TotalSeats,
		AvailableSeats: updatedEvent.AvailableSeats,
		Price:          updatedEvent.Price,
	}

	m.logger.Info("UpdateEvent successful", "event_id", req.EventId)
	return &pb.UpdateEventResponse{Event: pbEvent}, nil
}

func (m *CatalogManager) DeleteEvent(ctx context.Context, req *pb.DeleteEventRequest) (*pb.DeleteEventResponse, error) {
	m.logger.Info("DeleteEvent attempt", "event_id", req.EventId)

	id, err := uuid.Parse(req.EventId)
	if err != nil {
		m.logger.Warn("DeleteEvent failed: invalid event ID format", "event_id", req.EventId, "error", err)
		return nil, status.Error(codes.InvalidArgument, "invalid event id format")
	}

	err = m.repo.DeleteEvent(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			m.logger.Warn("DeleteEvent failed: event not found", "event_id", req.EventId)
			return nil, status.Error(codes.NotFound, "event not found")
		}

		m.logger.Error("DeleteEvent failed: database error", "event_id", req.EventId, "error", err)
		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	m.logger.Info("DeleteEvent successful", "event_id", req.EventId)
	return &pb.DeleteEventResponse{Success: true}, nil
}
