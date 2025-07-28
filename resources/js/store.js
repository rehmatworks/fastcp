require('./bootstrap');

import { createStore } from 'vuex';

export const store = createStore({
    state: {
        user: JSON.parse(localStorage.getItem('user')),
        busy: false,
        path: localStorage.getItem('file_path'),
    },
    mutations: {
        setPath(state, path) {
            state.path = path;
            localStorage.setItem('file_path', path);
        },
        setBusy(state, status) {
            state.busy = status;
        },
        setUser(state) {
            axios.defaults.baseURL = '/api/';
            axios.get('/account/').then((res) => {
                localStorage.setItem('user', JSON.stringify(res.data));
                state.user = res.data;
            }).catch((err) => {});
        },
        unsetUser(state) {
            localStorage.removeItem('user');
            state.user = null;
        }
    }
});