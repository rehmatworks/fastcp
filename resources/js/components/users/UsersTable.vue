<template>
    <div class="col-12">
        <div class="row mb-2">
            <div class="col-12">
                <h4 class="float-left">SSH/SFTP Users <span v-if="data">({{ data.count }})</span></h4>
                <router-link :to="{name: 'createuser'}" v-if="pagination" class="btn mb-1 btn-sm float-right btn-secondary">
                    <i class="fas fa-plus"></i> Add New
                </router-link>
            </div>
        </div>
        <div v-if="data && data.count > 0" class="table-responsive">
            <table class="table table-bordered">
                <thead class="bg-primary text-white">
                    <tr>
                        <th style="width: 20%">
                            Username & ID
                        </th>
                        <th>
                            Databases
                        </th>
                        <th>
                            Websites
                        </th>
                        <th>
                            Status
                        </th>
                        <th colspan="2">
                            Created
                        </th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-for="user in data.results" :key="user.id">
                        <td>{{ user.username }}<span v-if="user.uid" class="font-weight-bold text-secondary"> ({{ user.uid }})</span></td>
                        <td>{{ user.total_dbs }}/{{ user.max_dbs }}</td>
                        <td>{{ user.total_sites }}/{{ user.max_sites }}</td>
                        <td>
                            <span v-if="user.is_active" class="text-success">
                                <i class="fas fa-check-circle"></i> Active
                            </span>
                            <span v-else class="text-danger">
                                <i class="fas fa-times-circle"></i> Suspended
                            </span>
                        </td>
                        <td>
                            {{ user.date_joined }}
                        </td>
                        <td class="text-right">
                            <router-link :to="{name: 'user', params:{id: user.id}}" class="btn btn-sm btn-outline-primary">
                                <i class="fas fa-cog"></i> Settings
                            </router-link>
                        </td>
                    </tr>
                </tbody>
            </table>
        </div>
        <div v-else class="text-muted mb-3 border border-muted rounded p-5 text-center">
            No users found.
        </div>
        <nav v-if="pagination && data && data.count > 0" aria-label="User Pagination">
            <ul class="pagination float-right">
                <li class="page-item" @click="getUsers(data.links.previous)" :class="{'disabled': !data.links.previous }"><a class="page-link" href="javascript:void(0)">Previous</a></li>
                <li class="page-item" @click="getUsers(data.links.next)" :class="{'disabled': !data.links.next }"><a class="page-link" href="javascript:void(0)">Next</a></li>
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
        this.getUsers();
        const bus = (this.EventBus || window.EventBus);
        if (this.$route.name == 'users' && bus && bus.$on) {
            bus.$on('doSearch', this.getUsers);
        }
    },
    beforeDestroy() {
        const bus = (this.EventBus || window.EventBus);
        if (bus && bus.$off) {
            bus.$off('doSearch', this.getUsers);
        }
    },
    methods: {
        getUsers(page=1) {
            if(page == null) {
                return;
            }
            let search = document.getElementById('search-input').value;
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.get(`/ssh-users/?page=${page}&q=${search}`).then((res) => {
                _this.data = res.data;
                _this.$store.commit('setBusy', false);
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
            });
        }
    }
};
</script>
