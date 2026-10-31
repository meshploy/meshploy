package service

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// attachReferencedGroups gives each of a stack's services the published group
// of any other service in the stack whose variable it reads, as ${MEDUSA_DB_URL}
// reads the medusa-db service's. A compose file has no way to say so, and a
// service started without them runs with the reference as its literal value.
// It returns a warning for each reference that still has nothing behind it.
func (s *StackService) attachReferencedGroups(ctx context.Context, stackID uuid.UUID) []string {
	if s.deployment == nil || s.deployment.varGroups == nil {
		return nil
	}
	var services []db.Service
	if err := s.db.WithContext(ctx).Where("stack_id = ?", stackID).Find(&services).Error; err != nil {
		log.Printf("stack %s: read services: %v", stackID, err)
		return nil
	}
	ids := make([]uuid.UUID, 0, len(services))
	for _, svc := range services {
		ids = append(ids, svc.ID)
	}
	var groups []db.VariableGroup
	if err := s.db.WithContext(ctx).Where("service_id IN ?", ids).Find(&groups).Error; err != nil {
		log.Printf("stack %s: read groups: %v", stackID, err)
		return nil
	}
	owner := make(map[uuid.UUID]uuid.UUID, len(groups))
	groupIDs := make([]uuid.UUID, 0, len(groups))
	for _, g := range groups {
		owner[g.ID] = *g.ServiceID
		groupIDs = append(groupIDs, g.ID)
	}
	var items []db.VariableGroupItem
	if err := s.db.WithContext(ctx).Where("group_id IN ?", groupIDs).Find(&items).Error; err != nil {
		log.Printf("stack %s: read group items: %v", stackID, err)
		return nil
	}
	published := make(map[string]uuid.UUID, len(items))
	for _, it := range items {
		published[it.Key] = it.GroupID
	}

	var warnings []string
	for _, svc := range services {
		env := runtimeEnvVars(string(svc.EnvVars), 0)
		for _, e := range env {
			for _, ref := range envRefNames(e.Value) {
				group, ok := published[ref]
				if !ok || owner[group] == svc.ID {
					continue
				}
				if err := s.deployment.varGroups.Attach(ctx, svc.ID, group); err != nil {
					log.Printf("stack %s: attach %s to %s: %v", stackID, ref, svc.Name, err)
				}
			}
		}
		groupEnvs, _ := s.deployment.varGroups.CollectEnvVars(ctx, svc.ID)
		_, unknown, _ := resolveEnvRefs(mergeSecretEnvs(env, groupEnvs))
		if len(unknown) > 0 {
			sort.Strings(unknown)
			warnings = append(warnings, fmt.Sprintf("%s: nothing defines ${%s}, so it starts with that written literally",
				svc.Name, strings.Join(unknown, "}, ${")))
		}
	}
	return warnings
}
