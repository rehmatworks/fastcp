window.axios = require('axios');
window.axios.defaults.xsrfHeaderName = 'X-CSRFToken';
window.axios.defaults.xsrfCookieName = 'csrftoken';
window.axios.defaults.headers.common['X-Requested-With'] = 'XMLHttpRequest';