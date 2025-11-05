package root

import (
	"context"
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/docker/cagent/pkg/auth"
	"github.com/docker/cagent/pkg/session"
)

type usersFlags struct {
	sessionDB string
}

func newUsersCmd() *cobra.Command {
	var flags usersFlags

	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage users for the API server",
		Long:  `Manage user accounts for the cagent API server authentication system`,
	}

	// Add persistent flags
	cmd.PersistentFlags().StringVarP(&flags.sessionDB, "session-db", "s", "session.db", "Path to the session database")

	// Add subcommands
	cmd.AddCommand(newCreateUserCmd(&flags))
	cmd.AddCommand(newListUsersCmd(&flags))
	cmd.AddCommand(newDeleteUserCmd(&flags))
	cmd.AddCommand(newMakeAdminCmd(&flags))

	return cmd
}

func newCreateUserCmd(flags *usersFlags) *cobra.Command {
	var isAdmin bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new user",
		Long:  `Create a new user account for the API server`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get user input
			var email, name, password string

			fmt.Print("Email: ")
			_, _ = fmt.Scanln(&email)

			fmt.Print("Name: ")
			_, _ = fmt.Scanln(&name)

			fmt.Print("Password: ")
			passwordBytes, err := term.ReadPassword(syscall.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read password: %w", err)
			}
			password = string(passwordBytes)
			fmt.Println() // New line after password

			// Confirm password
			fmt.Print("Confirm Password: ")
			confirmBytes, err := term.ReadPassword(syscall.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read password confirmation: %w", err)
			}
			fmt.Println() // New line after password

			if password != string(confirmBytes) {
				return fmt.Errorf("passwords do not match")
			}

			// Open database
			store, userStore, err := openStores(flags.sessionDB)
			if err != nil {
				return err
			}
			defer store.(*session.SQLiteSessionStore).Close()

			// Create auth manager
			authManager := auth.NewManager(os.Getenv("CAGENT_JWT_SECRET"), userStore)

			// Create user
			user, err := authManager.RegisterUser(context.Background(), email, password, name)
			if err != nil {
				return fmt.Errorf("failed to create user: %w", err)
			}

			// Make admin if requested
			if isAdmin {
				user.IsAdmin = true
				if err := userStore.UpdateUser(context.Background(), user); err != nil {
					return fmt.Errorf("failed to set admin status: %w", err)
				}
			}

			fmt.Printf("✅ User created successfully\n")
			fmt.Printf("   ID: %s\n", user.ID)
			fmt.Printf("   Email: %s\n", user.Email)
			fmt.Printf("   Name: %s\n", user.Name)
			if user.IsAdmin {
				fmt.Printf("   Role: Admin\n")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&isAdmin, "admin", false, "Create user as admin")

	return cmd
}

func newListUsersCmd(flags *usersFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all users",
		Long:  `List all registered users in the system`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			store, userStore, err := openStores(flags.sessionDB)
			if err != nil {
				return err
			}
			defer store.(*session.SQLiteSessionStore).Close()

			// List users
			users, err := userStore.ListUsers(context.Background())
			if err != nil {
				return fmt.Errorf("failed to list users: %w", err)
			}

			if len(users) == 0 {
				fmt.Println("No users found")
				return nil
			}

			fmt.Printf("%-30s %-30s %-20s %-10s %-20s\n", "ID", "Email", "Name", "Admin", "Created")
			fmt.Println(String(130, "-"))
			for _, user := range users {
				adminStr := ""
				if user.IsAdmin {
					adminStr = "Yes"
				}
				fmt.Printf("%-30s %-30s %-20s %-10s %-20s\n",
					user.ID,
					user.Email,
					user.Name,
					adminStr,
					user.CreatedAt.Format("2006-01-02 15:04:05"),
				)
			}

			return nil
		},
	}
}

func newDeleteUserCmd(flags *usersFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete [email]",
		Short: "Delete a user",
		Long:  `Delete a user account by email`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]

			// Open database
			store, userStore, err := openStores(flags.sessionDB)
			if err != nil {
				return err
			}
			defer store.(*session.SQLiteSessionStore).Close()

			// Find user
			user, err := userStore.GetUserByEmail(context.Background(), email)
			if err != nil {
				return fmt.Errorf("user not found: %w", err)
			}

			// Confirm deletion
			fmt.Printf("Are you sure you want to delete user %s (%s)? [y/N]: ", user.Email, user.Name)
			var confirm string
			_, _ = fmt.Scanln(&confirm)
			if confirm != "y" && confirm != "Y" {
				fmt.Println("Deletion cancelled")
				return nil
			}

			// Delete user
			if err := userStore.DeleteUser(context.Background(), user.ID); err != nil {
				return fmt.Errorf("failed to delete user: %w", err)
			}

			fmt.Printf("✅ User %s deleted successfully\n", email)
			return nil
		},
	}
}

func newMakeAdminCmd(flags *usersFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "make-admin [email]",
		Short: "Grant admin privileges to a user",
		Long:  `Grant admin privileges to an existing user`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]

			// Open database
			store, userStore, err := openStores(flags.sessionDB)
			if err != nil {
				return err
			}
			defer store.(*session.SQLiteSessionStore).Close()

			// Find user
			user, err := userStore.GetUserByEmail(context.Background(), email)
			if err != nil {
				return fmt.Errorf("user not found: %w", err)
			}

			if user.IsAdmin {
				fmt.Printf("User %s is already an admin\n", email)
				return nil
			}

			// Update user
			user.IsAdmin = true
			if err := userStore.UpdateUser(context.Background(), user); err != nil {
				return fmt.Errorf("failed to update user: %w", err)
			}

			fmt.Printf("✅ User %s is now an admin\n", email)
			return nil
		},
	}
}

// Helper function to open stores
func openStores(sessionDB string) (session.Store, auth.UserStore, error) {
	// Open session store
	store, err := session.NewSQLiteSessionStore(sessionDB)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Get database connection
	sqliteStore, ok := store.(*session.SQLiteSessionStore)
	if !ok {
		return nil, nil, fmt.Errorf("invalid store type")
	}

	db := sqliteStore.GetDB()
	userStore := auth.NewSQLiteUserStore(db)

	return store, userStore, nil
}

// Helper function to create a string of repeated characters
func String(n int, char string) string {
	result := ""
	for range n {
		result += char
	}
	return result
}