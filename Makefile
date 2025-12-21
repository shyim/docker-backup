.PHONY: build build-css watch-css generate clean

# Build the entire project (CSS + templ + Go)
build: build-css generate
	go build -o docker-backup ./cmd/docker-backup

# Build production CSS with Tailwind
build-css:
	npm run build:css

# Watch CSS for development
watch-css:
	npm run watch:css

# Generate templ files
generate:
	templ generate

# Clean build artifacts
clean:
	rm -f docker-backup
	rm -f internal/dashboard/static/app.css
