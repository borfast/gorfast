# HTTP roadmap step 3: defer extraction

**Date:** 2026-10-26

**Status:** Approved by the project owner on 2026-10-26

**Context:** [Gorfast design](2026-09-27-gorfast-design.md) §§2, 8–10 and the [to-do example design](2026-10-19-example-todo-design.md)

## Intent

Roadmap step 3 originally proposed a Gorfast `http` package for routing, middleware, request binding and validation, and error mapping. The to-do example is the real consumer from which that package would be extracted. Its HTTP code does not yet show a reusable API that earns a new package. Defer the extraction until a concrete consumer need appears.

The project owner wants applications to remain compatible with `net/http` and free to organize their routes as they choose. Gorfast should not make a route-file layout or a router abstraction part of its runtime contract. A later CLI may generate an editable file that puts routes in one place; that is a starting point for an application, not a convention the library enforces.

## Evidence from the to-do consumer

- `examples/todo/internal/web/web.go` defines the routes together in `web.New` using `http.NewServeMux` and method-aware patterns. The application passes an `http.Handler` to the server already.
- The same function composes a body limit, form parsing, Sulis origin and CSRF middleware, and security headers. These are ordinary `http.Handler` wrappers; the code does not require a Gorfast router or middleware stack.
- `handlers.go` maps Sulis and to-do errors to status codes, messages, redirects and HTML pages. Those choices are specific to this application's flows and presentation. The roadmap assigns reusable HTML and JSON presentation to the later peer packages `view` and `api`.
- `middleware.go` validates sessions for protected routes and sets response headers. The signed-in identity and redirects are tied to this application's auth and page behavior.
- `web_test.go` exercises the browser flows, including CSRF, cross-origin requests, body limits, headers, session behavior and generic error responses. This working consumer supplies evidence about what the application needs; it does not establish that a second application would want the same library API.

## Goals, non-goals and anti-goals

**Goals**

- Record that roadmap step 3 is deferred, and state what evidence would reopen it.
- Preserve the to-do application's current behavior and standard-library composition.
- Keep route organization under the application author's control.
- Record the Sulis HTTP-middleware boundary as separate follow-up work.

**Non-goals for this step**

- A new `http` package, router, middleware stack, binder, validator or error mapper.
- Changes to the to-do application or Sulis.
- A CLI generator, implementation plan or product code.
- Decisions about the ownership or final API of origin and CSRF protection.

**Anti-goals**

- A wrapper that hides `net/http` or makes applications follow a fixed route layout.
- An HTTP-level transaction-per-request default. The service operation remains the transaction boundary under the Gorfast design §10.
- Moving a security check before its replacement has equivalent behavior and tests.

## Approaches considered

1. **Defer the package — chosen.** Keep the to-do app's explicit `net/http` composition. This avoids committing to an API from one consumer. A later application may repeat some code until there is enough evidence to extract it.
2. **Extract a small set of helpers now.** Body limits and headers are candidates, but their current values and error responses belong to the example. A package would need a contract and defaults before a second consumer has shown which parts are shared.
3. **Build the original full `http` package now.** A router, binding and validation layer, and error mapping would offer a common application shape. It would also duplicate working standard-library routing and decide presentation and policy boundaries ahead of demonstrated need.

## What remains in applications

The to-do app keeps `web.New`, its route table and its middleware composition. Other applications can use `net/http` directly and choose their own file layout. If the later CLI generates a single route file, the output is ordinary application code that the author can edit or replace. The CLI work is not part of this step.

Reopen an `http` package proposal when another real consumer shows repeated HTTP behavior that application code or the existing libraries handle poorly, or when a concrete security or composition defect calls for one shared implementation. The proposal should identify the repeated behavior, the contract and defaults it needs, and the user-visible improvement. Any future API must compose with `http.Handler` and `http.HandlerFunc`, keep feature packages independent under doctrine §2, and leave transaction ownership with services under §10. Define conformance tests if it introduces an interface.

## Separate Sulis boundary follow-up

The to-do app calls Sulis's `RequireSameOrigin` and `RequireCSRFToken` around the whole mux. They protect every POST, including sign-up and sign-in, so their policy is broader than authenticated operations. The project owner raised whether generic HTTP origin and CSRF protection belongs in an authentication library.

A separate Sulis design task should assess that boundary and choose the owner of those protections. This spec does not choose a destination or authorize moving them. Keep the current application behavior in place meanwhile. Any later move must preserve protection of unauthenticated forms, token reuse across tabs, cross-origin rejection, error handling and event reporting, and the existing browser-flow tests should prove the migration. The follow-up can evaluate Sulis's auth-specific cookie and session integration independently from its generic HTTP middleware.

## Completion and open decisions

This step is complete with the project owner's approval of this design as the record of the deferral. There is no implementation or test run in this step because it changes documentation only. A future consumer or the separate Sulis follow-up can start its own design and plan when its need is concrete.
