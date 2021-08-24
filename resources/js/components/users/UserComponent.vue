<template>
    <div v-if="user" class="row">
        <div class="col-12">
            <div class="row mb-2">
                <div class="col-12">
                    <h4>Manage User: {{ user.username }}</h4>
                </div>
            </div>
            <div v-if="user" class="row">
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
                                            <td style="width:40%;">User Name</td>
                                            <td>
                                                {{ user.username }}
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
                                        <tr>
                                            <td>Unix ID</td>
                                            <td>
                                                {{ user.uid }}
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>Websites</td>
                                            <td>
                                                {{ user.total_sites }}/{{ user.max_sites }}
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>Websites</td>
                                            <td>
                                                {{ user.total_dbs }}/{{ user.max_dbs }}
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>Storage Used</td>
                                            <td>
                                                {{ user.storage_used | prettyBytes }}/{{ user.max_storage | prettyBytes }}
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>Unix ID</td>
                                            <td>
                                                {{ user.uid }}
                                            </td>
                                        </tr>
                                    </tbody>
                                </table>
                            </div>
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
                            Do you want to delete this user? You can do so here. Beware
                            that this action is irreversible and you cannot undo once the
                            user is deleted. Entire data including websites and databases associated to this user
                            will be permanently deleted.
                        </div>
                        <div class="card-footer">
                            <button
                                v-if="!del"
                                @click="del = true"
                                class="btn btn-secondary"
                            >
                                Delete User
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
                                @click="deleteUser()"
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
            user: false,
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
        this.getUser();
    },
    methods: {
        resetPassword() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .post(`/ssh-users/${_this.$route.params.id}/reset-password/`)
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
        getUser() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .get(`/ssh-users/${_this.$route.params.id}/`)
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    _this.user = res.data;
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                    toastr.error('Cannot fetch the user data.');
                });
        },
        deleteUser() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios
                .delete(`/ssh-users/${_this.$route.params.id}/`)
                .then((res) => {
                    toastr.error('The user and associated data has been deleted.');
                    _this.$router.push({ name: 'users' });
                })
                .catch((err) => {
                    _this.$store.commit('setBusy', false);
                    toastr.error('An error occured while trying to delete this user.');
                });
        }
    },
};
</script>
