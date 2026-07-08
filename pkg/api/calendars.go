package api

import (
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

func (h *Handler) handleCalendar(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	volunteerID, ok := strings.CutSuffix(filename, ".ics")
	if !ok || volunteerID == "" {
		h.writeError(w, http.StatusNotFound, "calendar not found")
		return
	}

	volunteers, err := h.volunteers.ListVolunteers(h.cfg)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	volunteer := findVolunteerByID(volunteers, volunteerID)
	if volunteer == nil {
		// A miss may be a volunteer added since the cache was filled, and
		// their first request is typically a calendar app validating a new
		// subscription, so retry against a fresh roster before 404ing.
		if refresher, ok := h.volunteers.(VolunteerRefresher); ok {
			volunteers, err = refresher.RefreshVolunteers(h.cfg)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			volunteer = findVolunteerByID(volunteers, volunteerID)
		}
	}
	if volunteer == nil {
		h.writeError(w, http.StatusNotFound, "volunteer not found")
		return
	}

	shifts, err := services.ListShifts(r.Context(), h.store, h.volunteers, h.cfg, services.ListShiftsParams{}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	calendar, err := services.BuildVolunteerCalendar(
		services.FilterShiftsByVolunteer(shifts, volunteerID),
		*volunteer,
		h.cfg,
	)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="`+filename+`"`)
	if _, err := w.Write([]byte(calendar)); err != nil {
		h.logger.Error("Failed to write calendar response", zap.Error(err))
	}
}

func findVolunteerByID(volunteers []model.Volunteer, id string) *model.Volunteer {
	for i := range volunteers {
		if volunteers[i].ID == id {
			return &volunteers[i]
		}
	}
	return nil
}
