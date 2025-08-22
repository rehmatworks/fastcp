require('./bootstrap');

import { createStore } from 'vuex';

export const store = createStore({
    state: {
        user: JSON.parse(localStorage.getItem('user')),
        busy: false,
        // normalize stored path: decode up to 3 times to handle double-encoding left from older sessions
        path: (function(){
            try {
                let p = localStorage.getItem('file_path');
                if (!p) return p;
                // decode up to 3 times or until no percent-encoding remains
                for (let i=0;i<3;i++){
                    if (/%[0-9A-Fa-f]{2}/.test(p)) {
                        const d = decodeURIComponent(p);
                        if (d === p) break;
                        p = d;
                    } else break;
                }
                return p;
            } catch (e) { return localStorage.getItem('file_path'); }
        })(),
    },
    mutations: {
        setPath(state, path) {
            try {
                let p = path;
                if (p) {
                    for (let i=0;i<3;i++){
                        if (/%[0-9A-Fa-f]{2}/.test(p)) {
                            const d = decodeURIComponent(p);
                            if (d === p) break;
                            p = d;
                        } else break;
                    }
                }
                state.path = p;
                localStorage.setItem('file_path', p);
            } catch (e) {
                state.path = path;
                localStorage.setItem('file_path', path);
            }
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