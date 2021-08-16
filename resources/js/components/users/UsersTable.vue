<template>
    <div class="col-12">
        <div class="row mb-2">
            <div class="col-12">
                <h4 class="float-left">SSH Users (123)</h4>
                <button
                    v-if="pagination"
                    class="btn mb-1 btn-sm float-right btn-secondary"
                >
                    <i class="fas fa-plus"></i> Add New
                </button>
                <router-link
                    v-else
                    :to="{ name: 'databases' }"
                    class="btn mb-1 btn-sm float-right btn-outline-secondary"
                >
                    <i class="fas fa-list"></i> All Databases
                </router-link>
            </div>
        </div>
        <div class="table-responsive">
            <table class="table table-bordered">
                <thead class="bg-primary text-white">
                    <tr>
                        <th style="width: 30%">Database Name</th>
                        <th colspan="2">Database User</th>
                    </tr>
                </thead>
                <tbody>
                    <tr>
                        <td>wordpress</td>
                        <td>wordpress</td>
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
        <nav v-if="pagination" aria-label="Database Pagination">
            <ul class="pagination float-right">
                <li class="page-item">
                    <a class="page-link" href="javascript:void(0)">Previous</a>
                </li>
                <li class="page-item">
                    <a class="page-link" href="javascript:void(0)">Next</a>
                </li>
            </ul>
        </nav>
    </div>
</template>
<script>
export default {
    props: ['pagination'],
    created() {
        if (this.$route.name == 'users') {
            this.EventBus.$on('doSearch', this.getUsers);
        }
    },
    beforeDestroy() {
        this.EventBus.$off('doSearch', this.getUsers);
    },
    methods: {
        getUsers() {
            let _this = this;
            let search = document.getElementById('search-input').value;
            alert(search);
        },
    },
};
</script>
