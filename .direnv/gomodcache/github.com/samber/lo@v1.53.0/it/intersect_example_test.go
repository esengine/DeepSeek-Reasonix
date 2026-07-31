//go:build go1.23

package it

import (
	"fmt"
	"slices"
)

func ExampleWithoutBy() {
	type User struct {
		ID   int
		Name string
	}
	// original users
	users := values(
		User{ID: 1, Name: "Alice"},
		User{ID: 2, Name: "Bob"},
		User{ID: 3, Name: "Charlie"},
	)

	// exclude users with IDs 2 and 3
	excludedIDs := []int{2, 3}

	// extract function to get the user ID
	extractID := func(user User) int {
		return user.ID
	}

	// filtering users
	filteredUsers := WithoutBy(users, extractID, excludedIDs...)

	// output the filtered users
	fmt.Printf("%v", slices.Collect(filteredUsers))
	// Output:
	// [{1 Alice}]
}
