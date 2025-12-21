package templates

// FlashMessage represents a flash message to display
type FlashMessage struct {
	Type    string // "success" or "error"
	Message string
}

// BackupConfigInfo contains information about a backup configuration
type BackupConfigInfo struct {
	Name       string
	BackupType string
	Schedule   string
	Retention  int
	Storage    string
	NextRun    string
}

// ContainerInfo contains information about a container
type ContainerInfo struct {
	ID      string
	Name    string
	Notify  []string
	Backups []BackupConfigInfo
}

// IndexData contains data for the index page
type IndexData struct {
	ContainerCount int
	JobCount       int
	StorageCount   int
	Containers     []ContainerInfo
	Notifications  []NotificationInfo
	Flash          *FlashMessage
}

// BackupsData contains data for the backups page
type BackupsData struct {
	ContainerName string
	ConfigNames   []string                // Ordered list of config names
	BackupGroups  map[string][]BackupInfo // Backups grouped by config name
	Flash         *FlashMessage
}

// BackupInfo contains information about a backup
type BackupInfo struct {
	Key          string
	ConfigName   string
	Size         string
	LastModified string
}

// NotificationInfo contains information about a notification provider
type NotificationInfo struct {
	Name string
	Type string
}
