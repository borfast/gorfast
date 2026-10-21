package todo

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/borfast/sulis"

	"github.com/borfast/gorfast/data"
)

// The limits, counted in runes after trimming. MaxTodos keeps the in-memory
// sort and filter bounded.
const (
	MaxNameLength = 100
	MaxTextLength = 500
	MaxTodos      = 500
)

// Errors for input the service refuses. Each operation's own errors pass through.
var (
	ErrNameInvalid = errors.New("todo: a name must be 1 to 100 characters")
	ErrTextInvalid = errors.New("todo: a to-do must be 1 to 500 characters")
	ErrTooMany     = errors.New("todo: too many to-dos")

	errSecondFactor = errors.New("todo: sign-in needs a second factor, which this application does not support")
)

// Order is how List sorts.
type Order string

const (
	ByCreated Order = "created"
	ByText    Order = "text"
)

// SignedIn is a new session's raw token and when it expires.
type SignedIn struct {
	Token   string
	Expires time.Time
}

// Service is the application's operations. Each one runs in one transaction
// and uses only the stores that transaction is given.
type Service struct {
	run data.Runner[Repos]
}

// NewService returns a Service whose operations run through run.
func NewService(run data.Runner[Repos]) *Service {
	return &Service{run: run}
}

// SignUp registers the user and creates their profile, both or neither.
func (s *Service) SignUp(ctx context.Context, email, password, name string, ri sulis.RequestInfo) (SignedIn, error) {
	name, ok := clean(name, MaxNameLength)
	if !ok {
		return SignedIn{}, ErrNameInvalid
	}
	var in SignedIn
	err := s.run.Run(ctx, func(ctx context.Context, repos Repos) (error, error) {
		user, session, token, err := repos.Auth.Register(ctx, email, password, ri)
		if errors.Is(err, sulis.ErrRateLimited) {
			// Nothing was written, so commit: its event then logs without transaction=rolled_back.
			return err, nil
		}
		if err != nil {
			return nil, err
		}
		if err := repos.Profiles.CreateProfile(ctx, Profile{UserID: user.ID, Name: name}); err != nil {
			return nil, err
		}
		in = SignedIn{Token: token, Expires: session.ExpiresAt.UTC()}
		return nil, nil
	})
	if err != nil {
		return SignedIn{}, err
	}
	return in, nil
}

// signInOutcomes are refusals whose writes, such as a failed-attempt count,
// must commit. Any other error from Login rolls back.
var signInOutcomes = []error{
	sulis.ErrInvalidCredentials,
	sulis.ErrInvalidEmail,
	sulis.ErrRateLimited,
	sulis.ErrAccountDisabled,
	sulis.ErrAccountLocked,
	sulis.ErrEmailNotVerified,
}

// SignIn starts a session. Sulis's refusals, such as ErrInvalidCredentials,
// are returned unchanged and their writes commit.
func (s *Service) SignIn(ctx context.Context, email, password string, ri sulis.RequestInfo) (SignedIn, error) {
	var in SignedIn
	err := s.run.Run(ctx, func(ctx context.Context, repos Repos) (error, error) {
		res, err := repos.Auth.Login(ctx, email, password, ri)
		if err != nil {
			for _, outcome := range signInOutcomes {
				if errors.Is(err, outcome) {
					return err, nil
				}
			}
			return nil, err
		}
		if res.NeedsSecondFactor || res.Session == nil {
			return nil, errSecondFactor
		}
		in = SignedIn{Token: res.SessionToken, Expires: res.Session.ExpiresAt.UTC()}
		return nil, nil
	})
	if err != nil {
		return SignedIn{}, err
	}
	return in, nil
}

// SignOut deletes the session. Sulis refuses a session that is not userID's.
func (s *Service) SignOut(ctx context.Context, userID, sessionID uuid.UUID) error {
	return s.run.Run(ctx, func(ctx context.Context, repos Repos) (error, error) {
		return nil, repos.Auth.RevokeSession(ctx, userID, sessionID)
	})
}

// Name returns userID's name, or ErrNotFound.
func (s *Service) Name(ctx context.Context, userID uuid.UUID) (string, error) {
	var p Profile
	err := s.run.Run(ctx, func(ctx context.Context, repos Repos) (error, error) {
		var err error
		p, err = repos.Profiles.GetProfile(ctx, userID)
		return nil, err
	})
	if err != nil {
		return "", err
	}
	return p.Name, nil
}

// List returns userID's to-dos whose text contains filter, ignoring case.
// ByText sorts by text; any other order keeps creation order.
func (s *Service) List(ctx context.Context, userID uuid.UUID, filter string, order Order) ([]Todo, error) {
	var todos []Todo
	err := s.run.Run(ctx, func(ctx context.Context, repos Repos) (error, error) {
		var err error
		todos, err = repos.Todos.ListTodos(ctx, userID)
		return nil, err
	})
	if err != nil {
		return nil, err
	}

	if f := strings.ToLower(strings.TrimSpace(filter)); f != "" {
		todos = slices.DeleteFunc(todos, func(t Todo) bool {
			return !strings.Contains(strings.ToLower(t.Text), f)
		})
	}
	if order == ByText {
		slices.SortFunc(todos, func(a, b Todo) int {
			return cmp.Or(
				strings.Compare(strings.ToLower(a.Text), strings.ToLower(b.Text)),
				a.CreatedAt.Compare(b.CreatedAt),
				a.ID.Compare(b.ID),
			)
		})
	}
	return todos, nil
}

// Add creates a to-do, refusing with ErrTooMany once userID has MaxTodos.
func (s *Service) Add(ctx context.Context, userID uuid.UUID, text string) (Todo, error) {
	text, ok := clean(text, MaxTextLength)
	if !ok {
		return Todo{}, ErrTextInvalid
	}
	var added Todo
	err := s.run.Run(ctx, func(ctx context.Context, repos Repos) (error, error) {
		// The lock comes first, so concurrent adds for one user count one at a
		// time; the limit would not hold otherwise.
		if err := repos.Profiles.LockProfile(ctx, userID); err != nil {
			return nil, err
		}
		n, err := repos.Todos.CountTodos(ctx, userID)
		if err != nil {
			return nil, err
		}
		if n >= MaxTodos {
			return nil, ErrTooMany
		}
		now := time.Now().UTC()
		t := Todo{ID: uuid.NewV7(), OwnerID: userID, Text: text, CreatedAt: now, UpdatedAt: now}
		if err := repos.Todos.CreateTodo(ctx, t); err != nil {
			return nil, err
		}
		added = t
		return nil, nil
	})
	if err != nil {
		return Todo{}, err
	}
	return added, nil
}

// Edit replaces the text of userID's to-do id.
func (s *Service) Edit(ctx context.Context, userID, id uuid.UUID, text string) error {
	text, ok := clean(text, MaxTextLength)
	if !ok {
		return ErrTextInvalid
	}
	return s.update(ctx, userID, id, func(t *Todo) { t.Text = text })
}

// SetDone marks userID's to-do id as done or not done.
func (s *Service) SetDone(ctx context.Context, userID, id uuid.UUID, done bool) error {
	return s.update(ctx, userID, id, func(t *Todo) { t.Done = done })
}

// Delete returns ErrNotFound if userID has no to-do id.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return s.run.Run(ctx, func(ctx context.Context, repos Repos) (error, error) {
		return nil, repos.Todos.DeleteTodo(ctx, userID, id)
	})
}

// update changes userID's to-do id, or returns ErrNotFound. The lock comes
// first, so a concurrent change cannot be overwritten by one that read before it.
func (s *Service) update(ctx context.Context, userID, id uuid.UUID, change func(*Todo)) error {
	return s.run.Run(ctx, func(ctx context.Context, repos Repos) (error, error) {
		if err := repos.Profiles.LockProfile(ctx, userID); err != nil {
			return nil, err
		}
		t, err := repos.Todos.GetTodo(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		change(&t)
		t.UpdatedAt = time.Now().UTC()
		return nil, repos.Todos.UpdateTodo(ctx, t)
	})
}

// clean trims s and reports whether it then has 1 to limit runes.
func clean(s string, limit int) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= 1 && n <= limit
}
