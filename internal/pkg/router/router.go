package router

import (
	"fmt"
	"html/template"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	pbApi "github.com/luisguve/cheroproto-go/cheroapi"
	pbUsers "github.com/luisguve/cheroproto-go/userapi"
	"github.com/luisguve/cherosite/internal/pkg/livedata"
)

type Section struct {
	Client pbApi.CrudCheropatillaClient
	Id     string
	Name   string
}

func (s Section) preventDefault() error {
	if s.Client == nil {
		return fmt.Errorf("Got a nil section client.")
	}
	if s.Name == "" {
		return fmt.Errorf("Got an empty section name.")
	}
	if s.Id == "" {
		return fmt.Errorf("Got an empty section id.")
	}
	return nil
}

type Router struct {
	handler       *mux.Router
	upgrader      websocket.Upgrader
	templates     *template.Template
	store         sessions.Store
	hub           *livedata.Hub
	sections      map[string]Section
	usersClient   pbUsers.CrudUsersClient
	generalClient pbApi.CrudGeneralClient
}

func New(t *template.Template, users pbUsers.CrudUsersClient, general pbApi.CrudGeneralClient,
	sections []Section, s sessions.Store, hub *livedata.Hub, patillavatars []string) *Router {
	if t == nil {
		log.Fatal("Missing templates.")
	}
	if users == nil {
		log.Fatal("Missing users client.")
	}
	if general == nil {
		log.Fatal("Missing general client.")
	}
	if len(sections) == 0 {
		log.Fatal("There must be at least one section.")
	}
	if s == nil {
		log.Fatal("Missing sessions store.")
	}
	if hub == nil {
		log.Fatal("Missing hub.")
	}
	if len(patillavatars) == 0 {
		log.Fatal("No default patillavatars.")
	}
	defaultPics = patillavatars

	router := &Router{
		sections:      make(map[string]Section),
		templates:     t,
		store:         s,
		hub:           hub,
		usersClient:   users,
		generalClient: general,
		handler:       mux.NewRouter(),
		upgrader:      websocket.Upgrader{
			ReadBufferSize:  livedata.ReadBufferSize,
			WriteBufferSize: livedata.WriteBufferSize,
		},
	}

	for _, s := range sections {
		if err := s.preventDefault(); err != nil {
			log.Fatal(err)
		}
		router.sections[s.Id] = s
	}
	return router
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}

func (r *Router) SetupRoutes(upload, static string) {
	uploadDir = upload
	root := r.handler.PathPrefix("/").Subrouter().StrictSlash(true)
	// favicon (not found)
	root.Handle("/favicon.ico", http.NotFoundHandler())
	// serve assets
	root.PathPrefix("/"+uploadPath+"/").Handler(http.StripPrefix("/"+uploadPath+"/", http.FileServer(http.Dir("./"+uploadDir))))
	root.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./" + static))))
	//
	// WEBSOCKET
	//
	root.HandleFunc("/livenotifs", r.handleLiveNotifs).Methods("GET").Headers("X-Requested-With", "XMLHttpRequest")

	// handlers for homepage "/" features
	root.HandleFunc("/", r.onlyUsers(r.handleRoot)).Methods("GET")

	root.HandleFunc("/recyclefeed", r.onlyUsers(r.handleRecycleFeed)).Methods("GET").Headers("X-Requested-With", "XMLHttpRequest")

	root.HandleFunc("/recycleactivity", r.onlyUsers(r.handleRecycleMyActivity)).Methods("GET").Headers("X-Requested-With", "XMLHttpRequest")

	root.HandleFunc("/recyclesaved", r.onlyUsers(r.handleRecycleMySaved)).Methods("GET").Headers("X-Requested-With", "XMLHttpRequest")

	// explore page
	root.HandleFunc("/explore", r.handleExplore).Methods("GET")
	root.HandleFunc("/explore/recycle", r.handleExploreRecycle).Methods("GET").Headers("X-Requested-With", "XMLHttpRequest")

	// notifications
	root.HandleFunc("/readnotifs", r.onlyUsers(r.handleReadNotifs)).Methods("GET").Headers("X-Requested-With", "XMLHttpRequest")
	root.HandleFunc("/clearnotifs", r.onlyUsers(r.handleClearNotifs)).Methods("GET").Headers("X-Requested-With", "XMLHttpRequest")

	// follow event
	root.HandleFunc("/follow", r.onlyUsers(r.handleFollow)).Methods("POST").Queries("username", "{username:[a-zA-Z0-9_]+}")
	// unfollow event
	root.HandleFunc("/unfollow", r.onlyUsers(r.handleUnfollow)).Methods("POST").Queries("username", "{username:[a-zA-Z0-9_]+}")

	// get basic info of users either following or followers
	root.HandleFunc("/viewusers", r.handleViewUsers).Methods("GET").Queries("context", "{context:[a-z]+}", "userid", "{userid:[a-zA-Z0-9-]+}", "offset", "{offset:[0-9]+}").Headers("X-Requested-With", "XMLHttpRequest")

	// current user's profile page
	root.HandleFunc("/myprofile", r.onlyUsers(r.handleMyProfile)).Methods("GET")
	root.HandleFunc("/myprofile/update", r.onlyUsers(r.handleUpdateMyProfile)).Methods("PUT")

	// show other user's profile
	root.HandleFunc("/profile", r.handleViewUserProfile).Methods("GET").Queries("username", "{username:[a-zA-Z0-9_]+}")
	// recycle other user's activity
	root.HandleFunc("/profile/recycle", r.handleRecycleUserActivity).Methods("GET").Queries("userid", "{userid:[a-zA-Z0-9-]+}").Headers("X-Requested-With", "XMLHttpRequest")

	root.HandleFunc("/login", r.handleLogin).Methods("POST")
	root.HandleFunc("/signin", r.handleSignin).Methods("POST")
	root.HandleFunc("/logout", r.onlyUsers(r.handleLogout)).Methods("GET")

	// handlers for sections
	section := root.PathPrefix("/{section}").Subrouter()

	section.HandleFunc("", r.handleViewSection).Methods("GET")
	// create a thread
	section.HandleFunc("/new", r.onlyUsers(r.handleNewThread)).Methods("POST")
	// recycle section threads
	section.HandleFunc("/recycle", r.handleRecycleSection).Methods("GET")

	// handlers for threads
	thread := section.PathPrefix("/{thread}").Subrouter()
	thread.HandleFunc("", r.handleViewThread).Methods("GET")
	// recycle thread comments
	thread.HandleFunc("/recycle", r.handleRecycleComments).Methods("GET")
	// save thread "/{section}/{thread}/save"
	thread.HandleFunc("/save", r.onlyUsers(r.handleSave)).Methods("POST")
	// undo save thread "/{section}/{thread}/undosave"
	thread.HandleFunc("/undosave", r.onlyUsers(r.handleUndoSave)).Methods("POST")
	// delete thread "/{section}/{thread}/delete"
	thread.HandleFunc("/delete", r.onlyUsers(r.handleDeleteThread)).Methods("DELETE")

	// handlers for comments
	comments := thread.PathPrefix("/comment").Subrouter()
	// get 10 subcomments
	comments.HandleFunc("/", r.handleGetSubcomments).Methods("GET").Headers("X-Requested-With", "XMLHttpRequest").Queries("c_id", "{c_id:[a-zA-Z0-9]+}", "offset", "{offset:[0-9]+}")
	// post a subcomment
	comments.HandleFunc("/", r.onlyUsers(r.handlePostSubcomment)).Methods("POST").Queries("c_id", "{c_id:[a-zA-Z0-9]+}")
	// delete a subcomment
	// "/{section}/{thread}/comment/delete?c_id={c_id}&sc_id={sc_id}"
	comments.HandleFunc("/delete", r.onlyUsers(r.handleDeleteSubcomment)).Methods("DELETE").Queries("c_id", "{c_id:[a-zA-Z0-9]+}", "sc_id", "{sc_id:[a-zA-Z0-9]+}")
	// post a comment
	comments.HandleFunc("/", r.onlyUsers(r.handlePostComment)).Methods("POST")
	// delete a comment "/{section}/{thread}/comment/delete?c_id={c_id}"
	comments.HandleFunc("/delete", r.onlyUsers(r.handleDeleteComment)).Methods("DELETE").Queries("c_id", "{c_id:[a-zA-Z0-9]+}")

	// handlers for upvotes
	upvotes := thread.PathPrefix("/upvote").Subrouter()
	// upvote a subcomment
	upvotes.HandleFunc("/", r.onlyUsers(r.handleUpvoteSubcomment)).Methods("POST").Queries("c_id", "{c_id:[a-zA-Z0-9]+}", "sc_id", "{sc_id:[a-zA-Z0-9]+}")
	// upvote a comment
	upvotes.HandleFunc("/", r.onlyUsers(r.handleUpvoteComment)).Methods("POST").Queries("c_id", "{c_id:[a-zA-Z0-9]+}")
	// upvote a thread
	upvotes.HandleFunc("/", r.onlyUsers(r.handleUpvoteThread)).Methods("POST")

	// handlers for undoing upvotes
	undoUpvotes := thread.PathPrefix("/undoupvote").Subrouter()
	// undo upvote on a subcomment
	undoUpvotes.HandleFunc("/", r.onlyUsers(r.handleUndoUpvoteSubcomment)).Methods("POST").Queries("c_id", "{c_id:[a-zA-Z0-9]+}", "sc_id", "{sc_id:[a-zA-Z0-9]+}")
	// undo upvote on a comment
	undoUpvotes.HandleFunc("/", r.onlyUsers(r.handleUndoUpvoteComment)).Methods("POST").Queries("c_id", "{c_id:[a-zA-Z0-9]+}")
	// undo upvote on a thread
	undoUpvotes.HandleFunc("/", r.onlyUsers(r.handleUndoUpvoteThread)).Methods("POST")
}
