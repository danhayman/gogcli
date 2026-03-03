package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	admin "google.golang.org/api/admin/directory/v1"

	"github.com/steipete/gogcli/internal/errfmt"
	"github.com/steipete/gogcli/internal/googleapi"
	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

var newAdminDirectoryService = googleapi.NewAdminDirectory

const (
	adminRoleMember  = "MEMBER"
	adminRoleOwner   = "OWNER"
	adminRoleManager = "MANAGER"
)

// AdminCmd provides Google Workspace admin commands using the Admin SDK Directory API.
// Requires domain-wide delegation with a service account.
type AdminCmd struct {
	Users  AdminUsersCmd  `cmd:"" name:"users" help:"Manage Workspace users"`
	Groups AdminGroupsCmd `cmd:"" name:"groups" help:"Manage Workspace groups"`
}

// AdminUsersCmd manages Workspace users.
type AdminUsersCmd struct {
	List    AdminUsersListCmd    `cmd:"" name:"list" aliases:"ls" help:"List users in a domain"`
	Get     AdminUsersGetCmd     `cmd:"" name:"get" aliases:"info,show" help:"Get user details"`
	Create  AdminUsersCreateCmd  `cmd:"" name:"create" aliases:"add,new" help:"Create a new user"`
	Suspend AdminUsersSuspendCmd `cmd:"" name:"suspend" help:"Suspend a user account"`
}

// AdminUsersListCmd lists users in a Workspace domain.
type AdminUsersListCmd struct {
	Domain    string `name:"domain" help:"Domain to list users from (e.g., example.com)"`
	Max       int64  `name:"max" aliases:"limit" help:"Max results" default:"100"`
	Page      string `name:"page" aliases:"cursor" help:"Page token"`
	All       bool   `name:"all" aliases:"all-pages,allpages" help:"Fetch all pages"`
	FailEmpty bool   `name:"fail-empty" aliases:"non-empty,require-results" help:"Exit with code 3 if no results"`
}

func (c *AdminUsersListCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	domain := strings.TrimSpace(c.Domain)
	if domain == "" {
		return usage("domain required (e.g., --domain example.com)")
	}

	svc, err := newAdminDirectoryService(ctx, account)
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	fetch := func(pageToken string) ([]*admin.User, string, error) {
		call := svc.Users.List().
			Domain(domain).
			MaxResults(c.Max).
			Context(ctx)
		if strings.TrimSpace(pageToken) != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, "", wrapAdminDirectoryError(err, account)
		}
		return resp.Users, resp.NextPageToken, nil
	}

	var users []*admin.User
	nextPageToken := ""
	if c.All {
		all, err := collectAllPages(c.Page, fetch)
		if err != nil {
			return err
		}
		users = all
	} else {
		var err error
		users, nextPageToken, err = fetch(c.Page)
		if err != nil {
			return err
		}
	}

	if outfmt.IsJSON(ctx) {
		type item struct {
			Email     string `json:"email"`
			Name      string `json:"name,omitempty"`
			Suspended bool   `json:"suspended"`
			Admin     bool   `json:"admin"`
		}
		items := make([]item, 0, len(users))
		for _, u := range users {
			if u == nil {
				continue
			}
			items = append(items, item{
				Email:     u.PrimaryEmail,
				Name:      u.Name.FullName,
				Suspended: u.Suspended,
				Admin:     u.IsAdmin,
			})
		}
		if err := outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"users":         items,
			"nextPageToken": nextPageToken,
		}); err != nil {
			return err
		}
		if len(items) == 0 {
			return failEmptyExit(c.FailEmpty)
		}
		return nil
	}

	if len(users) == 0 {
		u.Err().Println("No users found")
		return failEmptyExit(c.FailEmpty)
	}

	w, flush := tableWriter(ctx)
	defer flush()
	fmt.Fprintln(w, "EMAIL\tNAME\tSUSPENDED\tADMIN")
	for _, user := range users {
		if user == nil {
			continue
		}
		suspended := "no"
		if user.Suspended {
			suspended = "yes"
		}
		isAdmin := "no"
		if user.IsAdmin {
			isAdmin = "yes"
		}
		name := ""
		if user.Name != nil {
			name = user.Name.FullName
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			sanitizeTab(user.PrimaryEmail),
			sanitizeTab(name),
			suspended,
			isAdmin,
		)
	}
	printNextPageHint(u, nextPageToken)
	return nil
}

// AdminUsersGetCmd gets details for a specific user.
type AdminUsersGetCmd struct {
	UserEmail string `arg:"" name:"userEmail" help:"User email (e.g., user@example.com)"`
}

func (c *AdminUsersGetCmd) Run(ctx context.Context, flags *RootFlags) error {
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	userEmail := strings.TrimSpace(c.UserEmail)
	if userEmail == "" {
		return usage("user email required")
	}

	svc, err := newAdminDirectoryService(ctx, account)
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	user, err := svc.Users.Get(userEmail).Context(ctx).Do()
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	if outfmt.IsJSON(ctx) {
		type item struct {
			Email       string   `json:"email"`
			Name        string   `json:"name,omitempty"`
			GivenName   string   `json:"givenName,omitempty"`
			FamilyName  string   `json:"familyName,omitempty"`
			Suspended   bool     `json:"suspended"`
			Admin       bool     `json:"admin"`
			Aliases     []string `json:"aliases,omitempty"`
			OrgUnitPath string   `json:"orgUnitPath,omitempty"`
			Creation    string   `json:"creationTime,omitempty"`
			LastLogin   string   `json:"lastLoginTime,omitempty"`
		}
		var aliases []string
		if user.Aliases != nil {
			aliases = user.Aliases
		}
		name := ""
		givenName := ""
		familyName := ""
		if user.Name != nil {
			name = user.Name.FullName
			givenName = user.Name.GivenName
			familyName = user.Name.FamilyName
		}
		return outfmt.WriteJSON(ctx, os.Stdout, item{
			Email:       user.PrimaryEmail,
			Name:        name,
			GivenName:   givenName,
			FamilyName:  familyName,
			Suspended:   user.Suspended,
			Admin:       user.IsAdmin,
			Aliases:     aliases,
			OrgUnitPath: user.OrgUnitPath,
			Creation:    user.CreationTime,
			LastLogin:   user.LastLoginTime,
		})
	}

	w, flush := tableWriter(ctx)
	defer flush()
	fmt.Fprintf(w, "Email:\t%s\n", user.PrimaryEmail)
	if user.Name != nil {
		fmt.Fprintf(w, "Name:\t%s\n", user.Name.FullName)
		fmt.Fprintf(w, "Given Name:\t%s\n", user.Name.GivenName)
		fmt.Fprintf(w, "Family Name:\t%s\n", user.Name.FamilyName)
	}
	fmt.Fprintf(w, "Suspended:\t%t\n", user.Suspended)
	fmt.Fprintf(w, "Admin:\t%t\n", user.IsAdmin)
	fmt.Fprintf(w, "Org Unit:\t%s\n", user.OrgUnitPath)
	fmt.Fprintf(w, "Created:\t%s\n", user.CreationTime)
	fmt.Fprintf(w, "Last Login:\t%s\n", user.LastLoginTime)
	if len(user.Aliases) > 0 {
		fmt.Fprintf(w, "Aliases:\t%s\n", strings.Join(user.Aliases, ", "))
	}
	return nil
}

// AdminUsersCreateCmd creates a new Workspace user.
type AdminUsersCreateCmd struct {
	Email      string `arg:"" name:"email" help:"User email (e.g., user@example.com)"`
	GivenName  string `name:"given" help:"Given (first) name"`
	FamilyName string `name:"family" help:"Family (last) name"`
	Password   string `name:"password" help:"Initial password"`
	ChangePwd  bool   `name:"change-password" help:"Require password change on first login"`
	OrgUnit    string `name:"org-unit" help:"Organization unit path"`
	Admin      bool   `name:"admin" help:"Make user an admin"`
}

func (c *AdminUsersCreateCmd) Run(ctx context.Context, flags *RootFlags) error {
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	email := strings.TrimSpace(c.Email)
	if email == "" {
		return usage("email required")
	}

	user := &admin.User{
		PrimaryEmail: email,
		Name: &admin.UserName{
			GivenName:  c.GivenName,
			FamilyName: c.FamilyName,
		},
		Password:                  c.Password,
		ChangePasswordAtNextLogin: c.ChangePwd,
		IsAdmin:                   c.Admin,
	}
	if c.OrgUnit != "" {
		user.OrgUnitPath = c.OrgUnit
	}

	if dryRunErr := dryRunExit(ctx, flags, "create user", user); dryRunErr != nil {
		return dryRunErr
	}

	svc, err := newAdminDirectoryService(ctx, account)
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	created, err := svc.Users.Insert(user).Context(ctx).Do()
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"email": created.PrimaryEmail,
			"id":    created.Id,
		})
	}

	u := ui.FromContext(ctx)
	u.Out().Printf("Created user: %s (ID: %s)", created.PrimaryEmail, created.Id)
	return nil
}

// AdminUsersSuspendCmd suspends a Workspace user account.
type AdminUsersSuspendCmd struct {
	UserEmail string `arg:"" name:"userEmail" help:"User email to suspend"`
}

func (c *AdminUsersSuspendCmd) Run(ctx context.Context, flags *RootFlags) error {
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	userEmail := strings.TrimSpace(c.UserEmail)
	if userEmail == "" {
		return usage("user email required")
	}

	if confirmErr := confirmDestructive(ctx, flags, fmt.Sprintf("suspend user %s", userEmail)); confirmErr != nil {
		return confirmErr
	}

	svc, err := newAdminDirectoryService(ctx, account)
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	user := &admin.User{
		Suspended: true,
	}

	updated, err := svc.Users.Update(userEmail, user).Context(ctx).Do()
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"email":     updated.PrimaryEmail,
			"suspended": updated.Suspended,
		})
	}

	u := ui.FromContext(ctx)
	u.Out().Printf("Suspended user: %s", updated.PrimaryEmail)
	return nil
}

// AdminGroupsCmd manages Workspace groups.
type AdminGroupsCmd struct {
	List    AdminGroupsListCmd    `cmd:"" name:"list" aliases:"ls" help:"List groups in a domain"`
	Members AdminGroupsMembersCmd `cmd:"" name:"members" help:"Manage group members"`
}

// AdminGroupsListCmd lists groups in a Workspace domain.
type AdminGroupsListCmd struct {
	Domain    string `name:"domain" help:"Domain to list groups from (e.g., example.com)"`
	Max       int64  `name:"max" aliases:"limit" help:"Max results" default:"100"`
	Page      string `name:"page" aliases:"cursor" help:"Page token"`
	All       bool   `name:"all" aliases:"all-pages,allpages" help:"Fetch all pages"`
	FailEmpty bool   `name:"fail-empty" aliases:"non-empty,require-results" help:"Exit with code 3 if no results"`
}

func (c *AdminGroupsListCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	domain := strings.TrimSpace(c.Domain)
	if domain == "" {
		return usage("domain required (e.g., --domain example.com)")
	}

	svc, err := newAdminDirectoryService(ctx, account)
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	fetch := func(pageToken string) ([]*admin.Group, string, error) {
		call := svc.Groups.List().
			Domain(domain).
			MaxResults(c.Max).
			Context(ctx)
		if strings.TrimSpace(pageToken) != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, "", wrapAdminDirectoryError(err, account)
		}
		return resp.Groups, resp.NextPageToken, nil
	}

	var groups []*admin.Group
	nextPageToken := ""
	if c.All {
		all, err := collectAllPages(c.Page, fetch)
		if err != nil {
			return err
		}
		groups = all
	} else {
		var err error
		groups, nextPageToken, err = fetch(c.Page)
		if err != nil {
			return err
		}
	}

	if outfmt.IsJSON(ctx) {
		type item struct {
			Email              string `json:"email"`
			Name               string `json:"name,omitempty"`
			Description        string `json:"description,omitempty"`
			DirectMembersCount int64  `json:"directMembersCount"`
		}
		items := make([]item, 0, len(groups))
		for _, g := range groups {
			if g == nil {
				continue
			}
			items = append(items, item{
				Email:              g.Email,
				Name:               g.Name,
				Description:        g.Description,
				DirectMembersCount: g.DirectMembersCount,
			})
		}
		if err := outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"groups":        items,
			"nextPageToken": nextPageToken,
		}); err != nil {
			return err
		}
		if len(items) == 0 {
			return failEmptyExit(c.FailEmpty)
		}
		return nil
	}

	if len(groups) == 0 {
		u.Err().Println("No groups found")
		return failEmptyExit(c.FailEmpty)
	}

	w, flush := tableWriter(ctx)
	defer flush()
	fmt.Fprintln(w, "EMAIL\tNAME\tMEMBERS\tDESCRIPTION")
	for _, group := range groups {
		if group == nil {
			continue
		}
		desc := group.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
			sanitizeTab(group.Email),
			sanitizeTab(group.Name),
			group.DirectMembersCount,
			sanitizeTab(desc),
		)
	}
	printNextPageHint(u, nextPageToken)
	return nil
}

// AdminGroupsMembersCmd manages group membership.
type AdminGroupsMembersCmd struct {
	List   AdminGroupsMembersListCmd   `cmd:"" name:"list" aliases:"ls" help:"List group members"`
	Add    AdminGroupsMembersAddCmd    `cmd:"" name:"add" aliases:"invite" help:"Add a member to a group"`
	Remove AdminGroupsMembersRemoveCmd `cmd:"" name:"remove" aliases:"rm,del,delete" help:"Remove a member from a group"`
}

// AdminGroupsMembersListCmd lists members of a group.
type AdminGroupsMembersListCmd struct {
	GroupEmail string `arg:"" name:"groupEmail" help:"Group email (e.g., engineering@example.com)"`
	Max        int64  `name:"max" aliases:"limit" help:"Max results" default:"100"`
	Page       string `name:"page" aliases:"cursor" help:"Page token"`
	All        bool   `name:"all" aliases:"all-pages,allpages" help:"Fetch all pages"`
	FailEmpty  bool   `name:"fail-empty" aliases:"non-empty,require-results" help:"Exit with code 3 if no results"`
}

func (c *AdminGroupsMembersListCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	groupEmail := strings.TrimSpace(c.GroupEmail)
	if groupEmail == "" {
		return usage("group email required")
	}

	svc, err := newAdminDirectoryService(ctx, account)
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	fetch := func(pageToken string) ([]*admin.Member, string, error) {
		call := svc.Members.List(groupEmail).
			MaxResults(c.Max).
			Context(ctx)
		if strings.TrimSpace(pageToken) != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, "", wrapAdminDirectoryError(err, account)
		}
		return resp.Members, resp.NextPageToken, nil
	}

	var members []*admin.Member
	nextPageToken := ""
	if c.All {
		all, err := collectAllPages(c.Page, fetch)
		if err != nil {
			return err
		}
		members = all
	} else {
		var err error
		members, nextPageToken, err = fetch(c.Page)
		if err != nil {
			return err
		}
	}

	if outfmt.IsJSON(ctx) {
		type item struct {
			Email string `json:"email"`
			Role  string `json:"role"`
			Type  string `json:"type"`
		}
		items := make([]item, 0, len(members))
		for _, m := range members {
			if m == nil {
				continue
			}
			items = append(items, item{
				Email: m.Email,
				Role:  m.Role,
				Type:  m.Type,
			})
		}
		if err := outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"members":       items,
			"nextPageToken": nextPageToken,
		}); err != nil {
			return err
		}
		if len(items) == 0 {
			return failEmptyExit(c.FailEmpty)
		}
		return nil
	}

	if len(members) == 0 {
		u.Err().Println("No members found")
		return failEmptyExit(c.FailEmpty)
	}

	w, flush := tableWriter(ctx)
	defer flush()
	fmt.Fprintln(w, "EMAIL\tROLE\tTYPE")
	for _, m := range members {
		if m == nil {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			sanitizeTab(m.Email),
			sanitizeTab(m.Role),
			sanitizeTab(m.Type),
		)
	}
	printNextPageHint(u, nextPageToken)
	return nil
}

// AdminGroupsMembersAddCmd adds a member to a group.
type AdminGroupsMembersAddCmd struct {
	GroupEmail  string `arg:"" name:"groupEmail" help:"Group email"`
	MemberEmail string `arg:"" name:"memberEmail" help:"Member email to add"`
	Role        string `name:"role" help:"Member role (MEMBER, MANAGER, OWNER)" default:"MEMBER"`
}

func (c *AdminGroupsMembersAddCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	groupEmail := strings.TrimSpace(c.GroupEmail)
	memberEmail := strings.TrimSpace(c.MemberEmail)
	if groupEmail == "" || memberEmail == "" {
		return usage("group email and member email required")
	}

	role := strings.ToUpper(c.Role)
	if role != adminRoleMember && role != adminRoleManager && role != adminRoleOwner {
		return usage("role must be MEMBER, MANAGER, or OWNER")
	}

	member := &admin.Member{
		Email: memberEmail,
		Role:  role,
	}

	if dryRunErr := dryRunExit(ctx, flags, fmt.Sprintf("add %s to %s as %s", memberEmail, groupEmail, role), member); dryRunErr != nil {
		return dryRunErr
	}

	svc, err := newAdminDirectoryService(ctx, account)
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	created, err := svc.Members.Insert(groupEmail, member).Context(ctx).Do()
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"email": created.Email,
			"role":  created.Role,
		})
	}

	u.Out().Printf("Added %s to %s as %s", created.Email, groupEmail, created.Role)
	return nil
}

// AdminGroupsMembersRemoveCmd removes a member from a group.
type AdminGroupsMembersRemoveCmd struct {
	GroupEmail  string `arg:"" name:"groupEmail" help:"Group email"`
	MemberEmail string `arg:"" name:"memberEmail" help:"Member email to remove"`
}

func (c *AdminGroupsMembersRemoveCmd) Run(ctx context.Context, flags *RootFlags) error {
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	groupEmail := strings.TrimSpace(c.GroupEmail)
	memberEmail := strings.TrimSpace(c.MemberEmail)
	if groupEmail == "" || memberEmail == "" {
		return usage("group email and member email required")
	}

	if confirmErr := confirmDestructive(ctx, flags, fmt.Sprintf("remove %s from %s", memberEmail, groupEmail)); confirmErr != nil {
		return confirmErr
	}

	svc, err := newAdminDirectoryService(ctx, account)
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	err = svc.Members.Delete(groupEmail, memberEmail).Context(ctx).Do()
	if err != nil {
		return wrapAdminDirectoryError(err, account)
	}

	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"removed": true,
			"email":   memberEmail,
			"group":   groupEmail,
		})
	}

	u := ui.FromContext(ctx)
	u.Out().Printf("Removed %s from %s", memberEmail, groupEmail)
	return nil
}

// wrapAdminDirectoryError provides helpful error messages for common Admin SDK issues.
func wrapAdminDirectoryError(err error, account string) error {
	errStr := err.Error()
	if strings.Contains(errStr, "accessNotConfigured") ||
		strings.Contains(errStr, "Admin SDK API has not been used") {
		return errfmt.NewUserFacingError("Admin SDK API is not enabled; enable it at: https://console.developers.google.com/apis/api/admin.googleapis.com/overview", err)
	}
	if strings.Contains(errStr, "insufficientPermissions") ||
		strings.Contains(errStr, "insufficient authentication scopes") ||
		strings.Contains(errStr, "Not Authorized") {
		return errfmt.NewUserFacingError("Insufficient permissions for Admin SDK API; ensure your service account has domain-wide delegation enabled with admin.directory.user and admin.directory.group scopes", err)
	}
	if strings.Contains(errStr, "domain_wide_delegation") ||
		strings.Contains(errStr, "invalid_grant") {
		return errfmt.NewUserFacingError("Domain-wide delegation not configured or invalid; ensure your service account has domain-wide delegation enabled in Google Workspace Admin Console", err)
	}
	if isConsumerAccount(account) {
		return errfmt.NewUserFacingError("Admin SDK Directory API requires a Google Workspace account with domain-wide delegation; consumer accounts (gmail.com/googlemail.com) are not supported.", err)
	}
	return err
}
