// Main Application Module
document.addEventListener('DOMContentLoaded', function() {
    // Check if user is authenticated
    if (Auth.isAuthenticated()) {
        initializeApp();
    } else {
        showAuthScreen();
    }
    
    // Add enter key handler for auth forms
    document.getElementById('login-password').addEventListener('keypress', function(e) {
        if (e.key === 'Enter') login();
    });
    
    document.getElementById('register-password').addEventListener('keypress', function(e) {
        if (e.key === 'Enter') register();
    });
});

// Initialize the main application
async function initializeApp() {
    showMainScreen();
    
    const user = await getCurrentUser();
    if (user) {
        document.getElementById('user-info').textContent = user.name || user.email;
    }
    
    // Load agents
    await loadAgents();
    
    // Set up periodic session refresh
    setInterval(() => {
        if (currentAgent) {
            loadSessions(currentAgent.name);
        }
    }, Config.SESSION_REFRESH_INTERVAL);
}

// Show authentication screen
function showAuthScreen() {
    document.getElementById('auth-container').style.display = 'flex';
    document.getElementById('main-container').style.display = 'none';
}

// Show main application screen
function showMainScreen() {
    document.getElementById('auth-container').style.display = 'none';
    document.getElementById('main-container').style.display = 'flex';
}

// Show loading overlay
function showLoading(show) {
    document.getElementById('loading-overlay').style.display = show ? 'flex' : 'none';
}

// Show toast notification
function showToast(message, type = 'info') {
    const container = document.getElementById('toast-container');
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = message;
    
    container.appendChild(toast);
    
    // Auto-remove after 5 seconds
    setTimeout(() => {
        toast.style.animation = 'slideOut 0.3s ease';
        setTimeout(() => {
            container.removeChild(toast);
        }, 300);
    }, 5000);
}

// Global error handler
window.addEventListener('error', function(e) {
    console.error('Global error:', e.error);
    showToast('An unexpected error occurred', 'error');
});

// Handle unhandled promise rejections
window.addEventListener('unhandledrejection', function(e) {
    console.error('Unhandled rejection:', e.reason);
    showToast('An unexpected error occurred', 'error');
});

// Add animation for toast slide out
const style = document.createElement('style');
style.textContent = `
    @keyframes slideOut {
        to {
            transform: translateX(120%);
            opacity: 0;
        }
    }
`;
document.head.appendChild(style);

// Mobile menu functions
function toggleMobileMenu() {
    const sidebar = document.getElementById('sidebar');
    const backdrop = document.getElementById('mobile-backdrop');
    
    sidebar.classList.toggle('mobile-open');
    backdrop.classList.toggle('active');
}

function closeMobileMenu() {
    const sidebar = document.getElementById('sidebar');
    const backdrop = document.getElementById('mobile-backdrop');
    
    sidebar.classList.remove('mobile-open');
    backdrop.classList.remove('active');
}

// Close mobile menu when selecting a session or creating new
function handleMobileMenuClose() {
    if (window.innerWidth <= 768) {
        closeMobileMenu();
    }
}

// Add resize listener to handle orientation changes
window.addEventListener('resize', function() {
    if (window.innerWidth > 768) {
        // Ensure menu is closed on desktop
        closeMobileMenu();
    }
});
