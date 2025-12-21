// Delete Modal Functions
function showDeleteModal(container, key) {
    document.getElementById('deleteModal').classList.remove('hidden');
    document.getElementById('deleteModal').classList.add('flex');
    document.getElementById('deleteBackupKey').textContent = key;
    document.getElementById('deleteForm').action = '/api/backup/delete?container=' + encodeURIComponent(container) + '&key=' + encodeURIComponent(key);
}

function hideDeleteModal() {
    document.getElementById('deleteModal').classList.add('hidden');
    document.getElementById('deleteModal').classList.remove('flex');
}

// Restore Modal Functions
function showRestoreModal(container, key) {
    document.getElementById('restoreModal').classList.remove('hidden');
    document.getElementById('restoreModal').classList.add('flex');
    document.getElementById('restoreBackupKey').textContent = key;
    document.getElementById('restoreForm').action = '/api/backup/restore?container=' + encodeURIComponent(container) + '&key=' + encodeURIComponent(key);
}

function hideRestoreModal() {
    document.getElementById('restoreModal').classList.add('hidden');
    document.getElementById('restoreModal').classList.remove('flex');
}

// Event Listeners
document.addEventListener('DOMContentLoaded', function() {
    // Close modal on escape key
    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') {
            var deleteModal = document.getElementById('deleteModal');
            if (deleteModal && !deleteModal.classList.contains('hidden')) {
                hideDeleteModal();
            }
            var restoreModal = document.getElementById('restoreModal');
            if (restoreModal && !restoreModal.classList.contains('hidden')) {
                hideRestoreModal();
            }
        }
    });

    // Close modal on backdrop click
    var deleteModal = document.getElementById('deleteModal');
    if (deleteModal) {
        deleteModal.addEventListener('click', function(e) {
            if (e.target === this) {
                hideDeleteModal();
            }
        });
    }

    var restoreModal = document.getElementById('restoreModal');
    if (restoreModal) {
        restoreModal.addEventListener('click', function(e) {
            if (e.target === this) {
                hideRestoreModal();
            }
        });
    }
});
