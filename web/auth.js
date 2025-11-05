// Authentication Module
const Auth = {
    // Get stored token
    getToken() {
        return localStorage.getItem(Config.TOKEN_STORAGE_KEY);
    },
    
    // Set token
    setToken(token) {
        localStorage.setItem(Config.TOKEN_STORAGE_KEY, token);
    },
    
    // Remove token
    removeToken() {
        localStorage.removeItem(Config.TOKEN_STORAGE_KEY);
    },
    
    // Get stored user
    getUser() {
        const userStr = localStorage.getItem(Config.USER_STORAGE_KEY);
        return userStr ? JSON.parse(userStr) : null;
    },
    
    // Set user
    setUser(user) {
        localStorage.setItem(Config.USER_STORAGE_KEY, JSON.stringify(user));
    },
    
    // Remove user
    removeUser() {
        localStorage.removeItem(Config.USER_STORAGE_KEY);
    },
    
    // Check if user is authenticated
    isAuthenticated() {
        return !!this.getToken();
    },
    
    // Logout
    logout() {
        this.removeToken();
        this.removeUser();
        window.location.reload();
    }
};

// Login function
async function login() {
    const email = document.getElementById('login-email').value.trim();
    const password = document.getElementById('login-password').value;
    
    if (!email || !password) {
        showToast('Please enter email and password', 'error');
        return;
    }
    
    showLoading(true);
    
    try {
        const response = await fetch(Config.getApiUrl('/api/auth/login'), {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ email, password })
        });
        
        const data = await response.json();
        
        if (response.ok) {
            Auth.setToken(data.token);
            Auth.setUser(data.user);
            showToast('Login successful!', 'success');
            initializeApp();
        } else {
            showToast(data.message || data.error || 'Login failed', 'error');
        }
    } catch (error) {
        console.error('Login error:', error);
        showToast('Failed to connect to server', 'error');
    } finally {
        showLoading(false);
    }
}

// Register function
async function register() {
    const name = document.getElementById('register-name').value.trim();
    const email = document.getElementById('register-email').value.trim();
    const password = document.getElementById('register-password').value;
    
    if (!name || !email || !password) {
        showToast('Please fill in all fields', 'error');
        return;
    }
    
    if (password.length < 8) {
        showToast('Password must be at least 8 characters', 'error');
        return;
    }
    
    showLoading(true);
    
    try {
        const response = await fetch(Config.getApiUrl('/api/auth/register'), {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ name, email, password })
        });
        
        const data = await response.json();
        
        if (response.ok) {
            showToast('Registration successful! Please login.', 'success');
            showLogin();
            // Clear registration form
            document.getElementById('register-name').value = '';
            document.getElementById('register-email').value = '';
            document.getElementById('register-password').value = '';
        } else {
            showToast(data.message || data.error || 'Registration failed', 'error');
        }
    } catch (error) {
        console.error('Registration error:', error);
        showToast('Failed to connect to server', 'error');
    } finally {
        showLoading(false);
    }
}

// Logout function
function logout() {
    Auth.logout();
}

// Show login form
function showLogin() {
    document.getElementById('login-form').style.display = 'block';
    document.getElementById('register-form').style.display = 'none';
}

// Show register form
function showRegister() {
    document.getElementById('login-form').style.display = 'none';
    document.getElementById('register-form').style.display = 'block';
}

// Get current user info
async function getCurrentUser() {
    const token = Auth.getToken();
    if (!token) return null;
    
    try {
        const response = await fetch(Config.getApiUrl('/api/auth/me'), {
            headers: {
                'Authorization': `Bearer ${token}`
            }
        });
        
        if (response.ok) {
            const user = await response.json();
            Auth.setUser(user);
            return user;
        } else if (response.status === 401) {
            Auth.logout();
            return null;
        }
    } catch (error) {
        console.error('Failed to get current user:', error);
    }
    
    return Auth.getUser();
}