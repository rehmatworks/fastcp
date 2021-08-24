<template>
    <div class="col-12">
        <div class="row mb-2">
            <div class="col-12">
                <h4 class="float-left">Websites <span v-if="data">({{ data.count }})</span></h4>
                <router-link :to="{name: 'deploysite'}" v-if="pagination" class="btn mb-1 btn-sm float-right btn-outline-primary">
                    <i class="fas fa-plus"></i> Deploy New
                </router-link>
                <router-link v-else :to="{name: 'websites'}" class="btn mb-1 btn-sm float-right btn-outline-primary">
                    <i class="fas fa-list"></i> All Websites
                </router-link>
            </div>
        </div>
        <div v-if="data && data.count > 0" class="table-responsive">
            <table class="table table-bordered">
                <thead class="bg-primary text-white">
                    <tr>
                        <th style="width: 30%">Label</th>
                        <th style="width: 20%">
                            PHP Version
                        </th>
                        <th colspan="2">
                            SSL
                        </th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-for="website in data.results" :key="website.id">
                        <td>{{ website.label }}</td>
                        <td><i class="fab fa-php"></i> {{ website.php }}</td>
                        <td>
                            <span v-if="website.has_ssl" class="text-success"><i class="fas fa-lock"></i> HTTPS</span>
                            <span v-else class="text-warning"><i class="fas fa-unlock"></i> HTTP</span>
                        </td>
                        <td class="text-right">
                            <router-link :to="{name: 'filemanager', params:{id: website.id}}" class="btn btn-sm btn-warning">
                                <i class="fas fa-folder"></i> Files
                            </router-link>
                            <router-link :to="{name: 'website', params:{id: website.id}}" class="btn btn-sm btn-primary">
                                <i class="fas fa-cog"></i> Settings
                            </router-link>
                        </td>
                    </tr>
                </tbody>
            </table>
        </div>
        <div v-else class="text-muted mb-3 border border-muted rounded p-5 text-center">
            No websites found.
        </div>
        <nav v-if="pagination && data && data.count > 0" aria-label="Pagination">
            <ul class="pagination float-right">
                <li class="page-item" @click="getWebsites(data.links.previous)" :class="{'disabled': !data.links.previous }"><a class="page-link" href="javascript:void(0)">Previous</a></li>
                <li class="page-item" @click="getWebsites(data.links.next)" :class="{'disabled': !data.links.next }"><a class="page-link" href="javascript:void(0)">Next</a></li>
            </ul>
        </nav>
    </div>
</template>
<script>
export default {
    props: ['pagination'],
    data() {
        return {
            data: false
        }
    },
    created() {
        this.getWebsites();
        if (this.$route.name == 'websites') {
            this.EventBus.$on('doSearch', this.getWebsites);
        }
    },
    beforeDestroy() {
        this.EventBus.$off('doSearch', this.getWebsites);
    },
    methods: {
        getWebsites(page=1) {
            if(page == null) {
                return;
            }
            let search = document.getElementById('search-input').value;
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.get(`/websites/?page=${page}&q=${search}`).then((res) => {
                _this.data = res.data;
                _this.$store.commit('setBusy', false);
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
            });
        }
    }
};
</script>
