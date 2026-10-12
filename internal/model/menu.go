package model

import (
	"sort"

	"github.com/google/uuid"
	"github.com/yuusufyan/go-common/pkg/database"
)

type Menu struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ParentID  *uuid.UUID `gorm:"type:uuid" json:"parent_id"`
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	Icon      string     `json:"icon"`
	SortOrder int        `json:"sort_order"`
	IsActive  bool       `json:"is_active"`
	Children  []*Menu    `gorm:"-" json:"children"`
	database.AuditModel
}

// BuildMenuTree nests menus under their parents, ordered by sort_order then name.
// Menus whose parent is not in the list are dropped, so hiding a parent hides its subtree.
func BuildMenuTree(menus []Menu) []*Menu {
	nodes := make(map[uuid.UUID]*Menu, len(menus))
	for i := range menus {
		m := menus[i]
		m.Children = []*Menu{}
		nodes[m.ID] = &m
	}

	roots := []*Menu{}
	for i := range menus {
		node := nodes[menus[i].ID]
		if node.ParentID == nil {
			roots = append(roots, node)
			continue
		}
		if parent, ok := nodes[*node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		}
	}

	sortMenus(roots)
	return roots
}

func sortMenus(menus []*Menu) {
	sort.SliceStable(menus, func(i, j int) bool {
		if menus[i].SortOrder != menus[j].SortOrder {
			return menus[i].SortOrder < menus[j].SortOrder
		}
		return menus[i].Name < menus[j].Name
	})
	for _, m := range menus {
		sortMenus(m.Children)
	}
}
