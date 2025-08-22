<template>
    <div class="row">
        <div class="col-12">
            <div
                v-if="$store.state.user && $store.state.user.is_root && resources"
                class="row"
            >
                <div class="col-xl-3 col-md-6 mb-4">
                    <router-link
                        :to="{ name: 'websites' }"
                        class="card text-decoration-none border-left-primary shadow h-100 py-2"
                    >
                        <div class="card-body">
                            <div class="row no-gutters align-items-center">
                                <div class="col mr-2">
                                    <div
                                        class="text-xs font-weight-bold text-primary text-uppercase mb-1"
                                    >
                                        Websites
                                    </div>
                                    <div class="h5 mb-0 font-weight-bold text-gray-800">
                                        {{ resources.stats.websites }}
                                    </div>
                                </div>
                                <div class="col-auto">
                                    <i class="fas fa-globe fa-2x text-gray-300"></i>
                                </div>
                            </div>
                        </div>
                    </router-link>
                </div>
                <div class="col-xl-3 col-md-6 mb-4">
                    <router-link
                        :to="{name: 'databases'}"
                        class="card text-decoration-none border-left-primary shadow h-100 py-2"
                    >
                        <div class="card-body">
                            <div class="row no-gutters align-items-center">
                                <div class="col mr-2">
                                    <div
                                        class="text-xs font-weight-bold text-primary text-uppercase mb-1"
                                    >
                                        Databases
                                    </div>
                                    <div class="h5 mb-0 font-weight-bold text-gray-800">
                                        {{ resources.stats.databases }}
                                    </div>
                                </div>
                                <div class="col-auto">
                                    <i class="fas fa-database fa-2x text-gray-300"></i>
                                </div>
                            </div>
                        </div>
                    </router-link>
                </div>
                <div class="col-xl-3 col-md-6 mb-4">
                    <router-link :to="{name: 'hardware'}" class="card text-decoration-none border-left-primary shadow h-100 py-2">
                        <div class="card-body">
                            <div class="row no-gutters align-items-center">
                                <div class="col mr-2">
                                    <div
                                        class="text-xs font-weight-bold text-primary text-uppercase mb-1"
                                    >
                                        Storage ({{ resources.disk.total | prettyBytes }})
                                    </div>
                                    <div class="row no-gutters align-items-center">
                                        <div class="col-auto">
                                            <div
                                                class="h5 mb-0 mr-3 font-weight-bold text-gray-800"
                                            >
                                                {{ resources.disk.percent }}%
                                            </div>
                                        </div>
                                        <div class="col">
                                            <div class="progress progress-sm mr-2">
                                                <div
                                                    class="progress-bar bg-primary"
                                                    role="progressbar"
                                                    :style="
                                                        'width: ' +
                                                        resources.disk.percent +
                                                        '%'
                                                    "
                                                    :aria-valuenow="
                                                        resources.disk.percent
                                                    "
                                                    aria-valuemin="0"
                                                    aria-valuemax="100"
                                                ></div>
                                            </div>
                                        </div>
                                    </div>
                                </div>
                                <div class="col-auto">
                                    <i class="fas fa-hdd fa-2x text-gray-300"></i>
                                </div>
                            </div>
                        </div>
                    </router-link>
                </div>
                <div class="col-xl-3 col-md-6 mb-4">
                    <router-link :to="{name: 'hardware'}" class="card text-decoration-none border-left-primary shadow h-100 py-2">
                        <div class="card-body">
                            <div class="row no-gutters align-items-center">
                                <div class="col mr-2">
                                    <div
                                        class="text-xs font-weight-bold text-primary text-uppercase mb-1"
                                    >
                                        RAM ({{ resources.ram.memory.total | prettyBytes }})
                                    </div>
                                    <div class="row no-gutters align-items-center">
                                        <div class="col-auto">
                                            <div
                                                class="h5 mb-0 mr-3 font-weight-bold text-gray-800"
                                            >
                                                {{ resources.ram.memory.percent }}%
                                            </div>
                                        </div>
                                        <div class="col">
                                            <div class="progress progress-sm mr-2">
                                                <div
                                                    class="progress-bar bg-primary"
                                                    role="progressbar"
                                                    :style="
                                                        'width: ' +
                                                        resources.ram.memory.percent +
                                                        '%'
                                                    "
                                                    :aria-valuenow="
                                                        resources.ram.memory.percent
                                                    "
                                                    aria-valuemin="0"
                                                    aria-valuemax="100"
                                                ></div>
                                            </div>
                                        </div>
                                    </div>
                                </div>
                                <div class="col-auto">
                                    <i class="fas fa-memory fa-2x text-gray-300"></i>
                                </div>
                            </div>
                        </div>
                    </router-link>
                </div>
            </div>
        </div>
        <websites-table class="mb-3"/>
        <databases-table/>
    </div>
</template>
<script>
import { defineComponent, onMounted, onBeforeUnmount } from 'vue';
import EventBus from '../../event-bus';
import DatabasesTable from '../databases/DatabasesTable.vue';
import WebsitesTable from '../websites/WebsitesTable.vue';

export default defineComponent({
    components: { DatabasesTable, WebsitesTable },
    data() {
        return {
            resources: false,
        };
    },
    mounted() {
        if(this.$store.state.user && this.$store.state.user.is_root) {
            this.getStats();
        }
    },
    created() {
        // TODO: Replace EventBus with mitt or provide/inject for Vue 3
        if (EventBus && EventBus.$on) {
            EventBus.$on('doSearch', this.browseWebsites);
        }
    },
    beforeUnmount() {
        // Vue 3: beforeUnmount replaces beforeDestroy
        if (EventBus && EventBus.$off) {
            EventBus.$off('doSearch', this.browseWebsites);
        }
    },
    methods: {
        browseWebsites() {
            this.$router.push({name: 'websites'});
        },
        getStats() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .get('/stats/common/')
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    _this.resources = res.data;
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                });
        },
    },
});
</script>
