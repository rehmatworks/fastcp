require('./bootstrap');
import { createApp } from 'vue';
import genRandPassword from './utils/password';

import { createRouter, createWebHistory } from 'vue-router';
import Loading from 'vue-loading-overlay';
import 'vue-loading-overlay/dist/css/index.css';
// import VueSelect from 'vue-select'; // Uncomment if using vue-select

window.axios.defaults.baseURL = '/api/';

import { routes } from './routes';
const router = createRouter({
    history: createWebHistory('/dashboard/'),
    routes: routes
});

import { store } from './store';

const app = createApp({
    data() {
        return {
            FM_ROOT: typeof FM_ROOT !== 'undefined' ? FM_ROOT : '',
            PMA_URL: typeof PMA_URL !== 'undefined' ? PMA_URL : ''
        };
    },
    mounted() {
        this.$store.commit('setUser');
    },
    methods: {
        genRandPassword(pwLen=15) {
            return genRandPassword(pwLen);
        },
        signOut() {
            let _this = this;
            axios.defaults.baseURL = '/dashboard/';
            axios.get('/sign-out/').then((res) => {
                _this.$store.commit('unsetUser');
                window.location = '';
            }).catch((err) => { });
        }
    }
});

app.use(router);
app.use(store);
app.component('loading', Loading);
// app.component('v-select', VueSelect.VueSelect); // Uncomment if using vue-select
app.component('usercreate-component', require('./components/generic/CreateuserComponent').default);
app.config.globalProperties.$filters = {
    floatformat(num) {
        return parseFloat(num).toFixed(2);
    },
    prettyBytes(num) {
        if (typeof num !== 'number' || isNaN(num)) {
            throw new TypeError('Expected a number');
        }
        var exponent;
        var unit;
        var neg = num < 0;
        var units = ['B', 'kB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'];
        if (neg) {
            num = -num;
        }
        if (num < 1) {
            return (neg ? '-' : '') + num + ' B';
        }
        exponent = Math.min(Math.floor(Math.log(num) / Math.log(1024)), units.length - 1);
        num = (num / Math.pow(1024, exponent)).toFixed(2) * 1;
        unit = units[exponent];
        return (neg ? '-' : '') + num + ' ' + unit;
    }
};

app.mount('#wrapper');