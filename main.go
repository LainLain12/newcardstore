package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type DailyFolder struct {
	Name string
}

type PageData struct {
	ActiveTab         string
	DailyFolders      []DailyFolder
	ActiveDailyFolder string
	DailyImages       []string
	WeeklyImages      []string
	SiteName          string
}

type ImagePageData struct {
	Src           string
	FileName      string
	Folder        string
	Kind          string // daily or weekly
	RelatedImages []string
	SiteName      string
	OGImage       string
	PageURL       string
	Title         string
	Description   string
}

const siteName = "Thai Card Store"

var templates *template.Template

func main() {
	loadTemplates()

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir("images"))))
	http.HandleFunc("/", galleryHandler)
	http.HandleFunc("/daily/", dailyFolderHandler)
	http.HandleFunc("/view", imageViewHandler)

	log.Println("Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func loadTemplates() {
	funcs := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
	}
	var err error
	templates, err = template.New("").Funcs(funcs).ParseGlob("templates/*.gohtml")
	if err != nil {
		log.Fatalf("error parsing templates: %v", err)
	}
}

func galleryHandler(w http.ResponseWriter, r *http.Request) {
	activeTab := r.URL.Query().Get("tab")
	if activeTab == "" {
		activeTab = "daily"
	}

	dailyFolders := listDailyFolders()
	weeklyImages := []string{}
	var activeDaily string
	var dailyImages []string

	if activeTab == "daily" {
		// choose folder: query param or first
		activeDaily = r.URL.Query().Get("folder")
		if activeDaily == "" && len(dailyFolders) > 0 {
			activeDaily = dailyFolders[0].Name
		}
		if activeDaily != "" {
			dailyImages = listImages(filepath.Join("images", "daily", activeDaily))
		}
	} else if activeTab == "weekly" {
		weeklyImages = listImages("images/weekly")
	}

	data := PageData{
		ActiveTab:         activeTab,
		DailyFolders:      dailyFolders,
		ActiveDailyFolder: activeDaily,
		DailyImages:       dailyImages,
		WeeklyImages:      weeklyImages,
		SiteName:          siteName,
	}

	if err := templates.ExecuteTemplate(w, "index.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// listDailyFolders returns sorted list of daily subfolders (names only)
func listDailyFolders() []DailyFolder {
	dailyBase := "images/daily"
	entries, err := os.ReadDir(dailyBase)
	if err != nil {
		return nil
	}
	var folders []DailyFolder
	for _, e := range entries {
		if e.IsDir() {
			folders = append(folders, DailyFolder{Name: e.Name()})
		}
	}
	sort.Slice(folders, func(i, j int) bool { return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name) })
	return folders
}

func listImages(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var imgs []string
	for _, e := range entries {
		if !e.IsDir() {
			name := e.Name()
			lower := strings.ToLower(name)
			if strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".gif") || strings.HasSuffix(lower, ".webp") {
				imgs = append(imgs, filepath.ToSlash(filepath.Join(dir, name)))
			}
		}
	}
	sort.Strings(imgs)
	return imgs
}

var safeFolderRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// dailyFolderHandler serves HTMX partial for a specific folder images
func dailyFolderHandler(w http.ResponseWriter, r *http.Request) {
	folder := strings.TrimPrefix(r.URL.Path, "/daily/")
	if !safeFolderRe.MatchString(folder) {
		http.Error(w, "invalid folder", http.StatusBadRequest)
		return
	}
	imgs := listImages(filepath.Join("images", "daily", folder))
	// Render minimal HTML snippet (no template dependency) for speed
	if len(imgs) == 0 {
		w.Write([]byte("<p class='text-gray-500'>No images in this folder.</p>"))
		return
	}
	var b strings.Builder
	for _, src := range imgs {
		viewURL := "/view?src=" + template.URLQueryEscaper(src)
		b.WriteString("<figure class='group relative overflow-hidden rounded-lg border bg-white shadow hover:shadow-md transition'>")
		b.WriteString("<a href='" + viewURL + "' class='block focus:outline-none'>")
		b.WriteString("<img loading='lazy' src='" + "/" + src + "' class='w-full h-40 object-cover group-hover:scale-105 transition' alt='" + template.HTMLEscapeString(filepath.Base(src)) + "' />")
		b.WriteString("</a>")
		// overlay buttons
		b.WriteString("<div class='absolute top-1 right-1 flex gap-1 opacity-0 group-hover:opacity-100 transition'>")
		b.WriteString("<button data-dl='" + "/" + src + "' class='dl-btn p-1.5 rounded-md bg-white/90 hover:bg-white shadow text-gray-700 text-xs font-medium'>Save</button>")
		b.WriteString("<button data-copy='" + "/" + src + "' class='copy-btn p-1.5 rounded-md bg-white/90 hover:bg-white shadow text-gray-700 text-xs font-medium'>Copy</button>")
		b.WriteString("</div>")
		b.WriteString("</figure>")
	}
	w.Header().Set("HX-Trigger", "folderLoaded")
	w.Write([]byte(b.String()))
}

// imageViewHandler renders a full screen view of one image with related images
func imageViewHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	src := q.Get("src") // expected like images/daily/<folder>/file or images/weekly/file
	if src == "" {
		http.NotFound(w, r)
		return
	}
	// security: ensure path stays under images
	if strings.Contains(src, "..") || !strings.HasPrefix(src, "images/") {
		http.Error(w, "invalid src", http.StatusBadRequest)
		return
	}
	fullPath := filepath.Clean(src)
	if _, err := os.Stat(fullPath); err != nil {
		http.NotFound(w, r)
		return
	}
	data := ImagePageData{Src: "/" + filepath.ToSlash(fullPath), FileName: filepath.Base(fullPath), SiteName: siteName}
	// Build absolute URLs for social preview
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	data.PageURL = scheme + "://" + r.Host + r.URL.RequestURI()
	data.OGImage = scheme + "://" + r.Host + data.Src
	data.Title = data.FileName + " - " + siteName
	data.Description = "View image from " + siteName
	parts := strings.Split(fullPath, "/")
	if len(parts) >= 3 && parts[1] == "daily" { // images/daily/<folder>/file
		data.Kind = "daily"
		data.Folder = parts[2]
		// gather related images in same folder
		related := listImages(filepath.Join("images", "daily", data.Folder))
		for _, rimg := range related {
			if "/"+rimg != data.Src {
				data.RelatedImages = append(data.RelatedImages, "/"+rimg)
			}
		}
	} else if len(parts) >= 2 && parts[1] == "weekly" { // images/weekly/file
		data.Kind = "weekly"
		related := listImages("images/weekly")
		for _, rimg := range related {
			if "/"+rimg != data.Src {
				data.RelatedImages = append(data.RelatedImages, "/"+rimg)
			}
		}
	} else {
		http.Error(w, "unsupported path", http.StatusBadRequest)
		return
	}
	if err := templates.ExecuteTemplate(w, "image.gohtml", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
