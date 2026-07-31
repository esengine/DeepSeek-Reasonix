package systray

import (
	"fmt"
	"sync"
	"testing"
)

func TestMenuItem_Remove(t *testing.T) {
	tests := []struct {
		name         string                // description of this test case
		menuItemFunc func() *MenuItem      // function to create the menu item to be removed
		checkFunc    func(*MenuItem) error // function to check the state of the menu item after removal
	}{
		{
			name: "Remove a menu item with no children",
			menuItemFunc: func() *MenuItem {
				return AddMenuItem("Test Item", "Tooltip for test item")
			},
			checkFunc: func(item *MenuItem) error {
				// Check if the item is removed from the menu
				if len(menuItems) != 0 {
					return fmt.Errorf("expected menuItems to be empty after removal, got: %v", menuItems)
				}
				return nil
			},
		},
		{
			name: "Remove a menu item with multiple children",
			menuItemFunc: func() *MenuItem {
				mainItem := AddMenuItem("Test Item", "Tooltip for test item")
				mainItem.AddSubMenuItem("Child Item 1", "Tooltip for child item 1")
				mainItem.AddSubMenuItem("Child Item 2", "Tooltip for child item 2")
				return mainItem
			},
			checkFunc: func(item *MenuItem) error {
				// Check if the item is removed from the menu
				if len(menuItems) != 0 {
					return fmt.Errorf("expected menuItems to be empty after removal, got: %v", menuItems)
				}
				return nil
			},
		},
		{
			name: "Remove a menu item with already channel set nil",
			menuItemFunc: func() *MenuItem {
				item := AddMenuItem("Test Item", "Tooltip for test item")
				item.ClickedCh = nil
				return item
			},
			checkFunc: func(item *MenuItem) error {
				// Check if the item is removed from the menu
				if len(menuItems) != 0 {
					return fmt.Errorf("expected menuItems to be empty after removal, got: %v", menuItems)
				}
				return nil
			},
		},
		{
			name: "Remove a menu item with already closed channel",
			menuItemFunc: func() *MenuItem {
				item := AddMenuItem("Test Item", "Tooltip for test item")
				close(item.ClickedCh)
				return item
			},
			checkFunc: func(item *MenuItem) error {
				// Check if the item is removed from the menu
				if len(menuItems) != 0 {
					return fmt.Errorf("expected menuItems to be empty after removal, got: %v", menuItems)
				}
				return nil
			},
		},
	}
	var wait sync.WaitGroup
	wait.Add(1)
	go Run(func() {
		SetTitle("Test Tray")
		SetTooltip("Test Tray Tooltip")
		wait.Done()
	}, nil)
	wait.Wait()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetMenu()
			var item *MenuItem
			item = tt.menuItemFunc()
			item.Remove()
			if err := tt.checkFunc(item); err != nil {
				t.Errorf("check failed: %v", err)
			}
		})
	}
	quit()
}
