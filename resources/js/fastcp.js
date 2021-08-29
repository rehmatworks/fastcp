require('./bootstrap');
import Vue from 'vue';

window.axios.defaults.baseURL = '/api/';

export const EventBus = new Vue();

import VueRouter from 'vue-router';
Vue.use(VueRouter);

import Loading from 'vue-loading-overlay';
import 'vue-loading-overlay/dist/vue-loading.css';
Vue.component('loading', Loading);

Vue.filter('floatformat', function(num) {
    return parseFloat(num).toFixed(2);
});

Vue.component('v-select', VueSelect.VueSelect);

// Global components
Vue.component('usercreate-component', require('./components/generic/CreateuserComponent').default);

Vue.filter('prettyBytes', function (num) {
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
});

import { routes } from './routes';
const router = new VueRouter({
    mode: 'history',
    routes: routes,
    base: '/dashboard/'
});

import { store } from './store';

Vue.mixin({
    data() {
        return {
            EventBus: EventBus,
            FM_ROOT: FM_ROOT,
            PMA_URL: PMA_URL
        }
    },
    methods: {
        genRandPassword(pwLen=15) {
            var pwdChars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
            return Array(pwLen).fill(pwdChars).map(function(x) { return x[Math.floor(Math.random() * x.length)] }).join('');
        }
    }
});


const app = new Vue({
    el: '#wrapper',
    router: router,
    store: store,
    mounted() {
        this.$store.commit('setUser');
    },
    methods: {
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