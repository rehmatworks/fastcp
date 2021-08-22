<template>
    <div class="col-12">
        <div class="row mb-2">
            <div class="col-12">
                <h4 class="float-left">Databases <span v-if="data">({{ data.count }})</span></h4>
                <router-link :to="{name: 'createdb'}" v-if="pagination" class="btn mb-1 btn-sm float-right btn-secondary">
                    <i class="fas fa-plus"></i> Add New
                </router-link>
                <router-link v-else :to="{name: 'databases'}" class="btn mb-1 btn-sm float-right btn-outline-primary">
                    <i class="fas fa-list"></i> All Databases
                </router-link>
            </div>
        </div>
        <div v-if="data && data.count > 0" class="table-responsive">
            <table class="table table-bordered">
                <thead class="bg-primary text-white">
                    <tr>
                        <th style="width: 30%">Name</th>
                        <th style="width: 20%">
                            Username
                        </th>
                        <th colspan="2">
                            Created
                        </th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-for="database in data.results" :key="database.id">
                        <td>{{ database.name }}</td>
                        <td>{{ database.username }}</td>
                        <td>
                            {{ database.created }}
                        </td>
                        <td class="text-right">
                            <button class="btn btn-sm btn-outline-primary">
                                <i class="fas fa-cog"></i>
                            </button>
                            <button class="btn btn-sm btn-outline-info">
                                <i class="fas fa-download"></i>
                            </button>
                        </td>
                    </tr>
                </tbody>
            </table>
        </div>
        <div v-else class="text-muted mb-3 border border-muted rounded p-5 text-center">
            No databases found.
        </div>
        <nav v-if="pagination && data && data.count > 0" aria-label="Database Pagination">
            <ul class="pagination float-right">
                <li class="page-item" @click="getDatabases(data.links.previous)" :class="{'disabled': !data.links.previous }"><a class="page-link" href="javascript:void(0)">Previous</a></li>
                <li class="page-item" @click="getDatabases(data.links.next)" :class="{'disabled': !data.links.next }"><a class="page-link" href="javascript:void(0)">Next</a></li>
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
        this.getDatabases();
        if (this.$route.name == 'databases') {
            this.EventBus.$on('doSearch', this.getDatabases);
        }
    },
    beforeDestroy() {
        this.EventBus.$off('doSearch', this.getDatabases);
    },
    methods: {
        getDatabases(page=1) {
            if(page == null) {
                return;
            }
            let search = document.getElementById('search-input').value;
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.get(`/databases/?page=${page}&q=${search}`).then((res) => {
                _this.data = res.data;
                _this.$store.commit('setBusy', false);
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
            });
        }
    }
};
</script>
