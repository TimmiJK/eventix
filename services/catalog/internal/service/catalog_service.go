package catalog

import (
	"context"
	"database/sql"
	"errors"
	"eventix/proto/catalog/pb"
	"eventix/services/catalog/internal/catalogctx"
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
		PriceMinor:     event.PriceMinor,
		Currency:       event.Currency,
		Version:        event.Version,
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
			PriceMinor:     val.PriceMinor,
			Currency:       val.Currency,
			Version:        val.Version,
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

var adminRoles = map[string]struct{}{
	"admin":     {},
	"organizer": {},
}

func (m *CatalogManager) CreateEvent(ctx context.Context, req *pb.CreateEventRequest) (*pb.CreateEventResponse, error) {
	m.logger.Info("CreateEvent attempt", "title", req.Title, "description", req.Description)

	userID, ok1 := catalogctx.UserID(ctx)
	role, ok2 := catalogctx.UserRole(ctx)
	if !ok1 || !ok2 {
		m.logger.Warn("Unauthenticated", "user_id", userID.String(), "role", role)
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	if _, ok := adminRoles[role]; !ok {
		m.logger.Warn("Not allow method", "user_id", userID.String(), "role", role)
		return nil, status.Error(codes.PermissionDenied, "method is not allowed")
	}

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
	if req.PriceMinor <= 0 {
		m.logger.Warn("CreateEvent failed: price cannot be negative", "title", req.Title, "description", req.Description)
		return nil, status.Error(codes.InvalidArgument, "price_minor must be positive")
	}
	if req.TotalSeats < 0 {
		m.logger.Warn("CreateEvent failed: totalSeats cannot be negative", "title", req.Title, "description", req.Description)
		return nil, status.Error(codes.InvalidArgument, "totalSeats cannot be negative")
	}

	if eventDate.Before(time.Now().Add(-1 * time.Hour)) {
		return nil, status.Error(codes.InvalidArgument, "eventDate cannot be in the past")
	}

	if req.Currency != "RUB" {
		return nil, status.Error(codes.InvalidArgument, "currency could be only \"RUB\"")
	}

	event := &models.Event{
		Title:          req.Title,
		Description:    req.Description,
		Venue:          req.Venue,
		EventDate:      eventDate,
		TotalSeats:     req.TotalSeats,
		AvailableSeats: req.TotalSeats,
		PriceMinor:     req.PriceMinor,
		Currency:       req.Currency,
	}

	createdEvent, err := m.repo.CreateEvent(ctx, event, userID)
	if err != nil {
		if errors.Is(err, repository.ErrEventTitleTaken) {
			m.logger.Warn("CreateEvent failed: event already exists", "title", req.Title, "description", req.Description)
			return nil, status.Error(codes.AlreadyExists, "event title already exists")
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
		PriceMinor:     createdEvent.PriceMinor,
		Currency:       createdEvent.Currency,
		Version:        createdEvent.Version,
	}

	m.logger.Info("CreateEvent successful", "event_id", createdEvent.ID.String())
	return &pb.CreateEventResponse{Event: pbEvent}, nil
}

func (m *CatalogManager) UpdateEvent(ctx context.Context, req *pb.UpdateEventRequest) (*pb.UpdateEventResponse, error) {
	m.logger.Info("UpdateEvent attempt", "event_id", req.EventId)

	userID, ok1 := catalogctx.UserID(ctx)
	role, ok2 := catalogctx.UserRole(ctx)
	if !ok1 || !ok2 {
		m.logger.Warn("Unauthenticated", "user_id", userID.String(), "role", role)
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	if _, ok := adminRoles[role]; !ok {
		m.logger.Warn("Not allow method", "user_id", userID.String(), "role", role)
		return nil, status.Error(codes.PermissionDenied, "method is not allowed")
	}

	eventUUID, err := uuid.Parse(req.EventId)
	if err != nil {
		m.logger.Warn("UpdateEvent failed: invalid event ID format", "event_id", req.EventId, "error", err)
		return nil, status.Error(codes.InvalidArgument, "invalid event id format")
	}

	if req.ExpectedVersion <= 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_version must be positive")
	}

	paths := req.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		return nil, status.Error(codes.InvalidArgument, "update_mask is required")
	}

	var patch models.EventPatch

	for _, path := range paths {
		switch path {
		case "title":
			if strings.TrimSpace(req.Title) == "" {
				return nil, status.Error(codes.InvalidArgument, "title is required")
			}
			if len(req.Title) > 200 {
				return nil, status.Error(codes.InvalidArgument, "title is too long")
			}

			patch.Title = &req.Title
		case "description":
			patch.Description = &req.Description
		case "venue":
			patch.Venue = &req.Venue
		case "event_date":
			eventDate, err := time.Parse(time.RFC3339, req.EventDate)
			if err != nil {
				m.logger.Warn("UpdateEvent failed: invalid event date format", "event_date", req.EventDate, "error", err)
				return nil, status.Error(codes.InvalidArgument, "invalid event date format")
			}
			if eventDate.Before(time.Now()) {
				return nil, status.Error(codes.InvalidArgument, "eventDate cannot be in the past")
			}

			patch.EventDate = &eventDate
		case "total_seats":
			if req.TotalSeats < 0 {
				return nil, status.Error(codes.InvalidArgument, "totalSeats cannot be negative")
			}

			patch.TotalSeats = &req.TotalSeats
		case "price_minor":
			if req.PriceMinor <= 0 {
				return nil, status.Error(codes.InvalidArgument, "price_minor must be positive")
			}

			patch.PriceMinor = &req.PriceMinor
		case "currency":
			if req.Currency != "RUB" {
				return nil, status.Error(codes.InvalidArgument, "currency must be \"RUB\"")
			}
			patch.Currency = &req.Currency
		default:
			m.logger.Warn("UpdateEvent failed: unknown field in mask", "field", path)
			return nil, status.Errorf(codes.InvalidArgument, "unknown field: %s", path)
		}
	}

	updatedEvent, err := m.repo.UpdateEvent(ctx, eventUUID, &patch, req.ExpectedVersion, userID, role == "admin")
	switch {
	case errors.Is(err, repository.ErrEventTitleTaken):
		return nil, status.Error(codes.AlreadyExists, "event title already exists")
	case errors.Is(err, sql.ErrNoRows):
		return nil, status.Error(codes.NotFound, "event not found")
	case errors.Is(err, repository.ErrEventVersionConflict):
		return nil, status.Error(codes.Aborted, "event was changed, reload and retry")
	case errors.Is(err, repository.ErrEventNotEditable):
		return nil, status.Error(codes.FailedPrecondition, "event is not editable")
	case errors.Is(err, repository.ErrCapacityBelowOccupied):
		return nil, status.Error(codes.FailedPrecondition, "capacity cannot be below occupied seats")
	case err != nil:
		m.logger.ErrorContext(ctx, "UpdateEvent failed", "event_id", req.EventId, "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	pbEvent := &pb.Event{
		Id:             updatedEvent.ID.String(),
		Title:          updatedEvent.Title,
		Description:    updatedEvent.Description,
		Venue:          updatedEvent.Venue,
		EventDate:      updatedEvent.EventDate.Format(time.RFC3339),
		TotalSeats:     updatedEvent.TotalSeats,
		AvailableSeats: updatedEvent.AvailableSeats,
		PriceMinor:     updatedEvent.PriceMinor,
		Currency:       updatedEvent.Currency,
		Version:        updatedEvent.Version,
	}

	m.logger.Info("UpdateEvent successful", "event_id", req.EventId)
	return &pb.UpdateEventResponse{Event: pbEvent}, nil
}

func (m *CatalogManager) DeleteEvent(ctx context.Context, req *pb.DeleteEventRequest) (*pb.DeleteEventResponse, error) {
	m.logger.Info("DeleteEvent attempt", "event_id", req.EventId)

	userID, ok1 := catalogctx.UserID(ctx)
	role, ok2 := catalogctx.UserRole(ctx)
	if !ok1 || !ok2 {
		m.logger.Warn("Unauthenticated", "user_id", userID.String(), "role", role)
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	if _, ok := adminRoles[role]; !ok {
		m.logger.Warn("Not allow method", "user_id", userID.String(), "role", role)
		return nil, status.Error(codes.PermissionDenied, "method is not allowed")
	}

	id, err := uuid.Parse(req.EventId)
	if err != nil {
		m.logger.Warn("DeleteEvent failed: invalid event ID format", "event_id", req.EventId, "error", err)
		return nil, status.Error(codes.InvalidArgument, "invalid event id format")
	}

	if req.ExpectedVersion <= 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_version must be positive")
	}

	err = m.repo.DeleteEvent(ctx, id, req.ExpectedVersion, userID, role == "admin")
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, status.Error(codes.NotFound, "event not found")
	case errors.Is(err, repository.ErrEventNotDeletable):
		return nil, status.Error(codes.FailedPrecondition, "event can't be deleted")
	case errors.Is(err, repository.ErrEventVersionConflict):
		return nil, status.Error(codes.Aborted, "event was changed, reload and retry")
	case err != nil:
		m.logger.ErrorContext(ctx, "DeleteEvent failed", "event_id", req.EventId, "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	m.logger.Info("DeleteEvent successful", "event_id", req.EventId)
	return &pb.DeleteEventResponse{Success: true}, nil
}
