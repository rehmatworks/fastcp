<template>
    <div class="row">
        <div class="col-12">
            <div class="row mb-2">
                <div class="col-12">
                    <h4>Manage Database: #{{ $route.params.id }}</h4>
                </div>
            </div>
            <div v-if="database" class="row">
                <div class="col-md-8">
                    <div class="card mb-3">
                        <div class="card-header bg-primary text-light">
                            General Details
                        </div>
                        <div class="card-body">
                            <div class="responsive-table">
                                <table class="table table-borderless table-striped">
                                    <tbody>
                                        <tr>
                                            <td>Database Name</td>
                                            <td>
                                                {{ database.name }}
                                            </td>
                                        </tr>
                                        <tr>
                                            <td style="width: 40%">Database User</td>
                                            <td>
                                                <span>{{ database.username }}</span>
                                                <small>
                                                    <a
                                                        v-if="!reset"
                                                        @click="reset = true"
                                                        href="javascript:void(0)"
                                                        class="text-danger"
                                                    >
                                                        <i class="fas fa-redo"></i> Reset
                                                        Password
                                                    </a>
                                                    <a
                                                        v-if="reset"
                                                        @click="reset = false"
                                                        href="javascript:void(0)"
                                                        class="text-success"
                                                    >
                                                        Cancel
                                                    </a>
                                                    <a
                                                        v-if="reset"
                                                        @click="resetPassword()"
                                                        href="javascript:void(0)"
                                                        class="text-danger"
                                                    >
                                                        Reset
                                                    </a>
                                                </small>
                                            </td>
                                        </tr>
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    </div>
                    <div class="card mt-3">
                        <div class="card-header text-light bg-primary">
                            Backups
                            <button class="btn btn-sm btn-primary float-right">
                                <i class="fas fa-upload"></i> Import
                            </button>
                            <button class="btn btn-sm btn-primary float-right">
                                <i class="fas fa-download"></i> Export
                            </button>
                        </div>
                        <div class="card-body">

                        </div>
                    </div>
                    <div class="card mt-3">
                        <div
                            :class="{ 'bg-secondary': !del, 'bg-danger': del }"
                            class="card-header text-light"
                        >
                            Danger Zone
                        </div>
                        <div class="card-body">
                            Do you want to delete this database? You can do so here. Beware
                            that this action is irreversible and you cannot undo once the
                            database is deleted. Entire data associated to this database
                            will be permanently lost.
                        </div>
                        <div class="card-footer">
                            <button
                                v-if="!del"
                                @click="del = true"
                                class="btn btn-secondary"
                            >
                                Delete Database
                            </button>
                            <button
                                v-if="del"
                                @click="del = false"
                                class="btn btn-success"
                            >
                                Don't Delete
                            </button>
                            <button
                                v-if="del"
                                @click="deleteDatabase()"
                                class="btn btn-danger"
                            >
                                Confirm Deletion
                            </button>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
</template>
<script>
export default {
    data() {
        return {
            database: false,
            del: false,
            reset: false,
            change_php: false,
            php_versions: [],
            del_dom: false,
            new_domain: '',
            add_dom: false,
            errors: {}
        };
    },
    created() {
        this.getDatabase();
    },
    methods: {
        resetPassword() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .post(`/databases/${_this.$route.params.id}/reset-password/`)
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    toastr.success('SSH/SFTP password has been updated.');
                    _this.reset = false;
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                    toastr.error('SSH/SFTP password cannot be updated.');
                });
        },
        getDatabase() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .get(`/databases/${_this.$route.params.id}/`)
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    _this.database = res.data;
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                    toastr.error('Cannot fetch the database data.');
                });
        },
        deleteDatabase() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .delete(`/databases/${_this.$route.params.id}/`)
                .then((res) => {
                    toastr.error('The database and associated data has been deleted.');
                    _this.$router.push({ name: 'databases' });
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                    toastr.error('An error occured while trying to delete this database.');
                });
        }
    },
};
</script>
