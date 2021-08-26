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
                            <p v-if="new_password">New password is <small class="text-info font-weight-bold">{{ new_password }}</small> and it is shown to you this single time. <a @click="new_password=false" class="text-danger text-decoration-none" style="font-size:14px;" href="javascript:void(0)"><i class="fas fa-times-circle"></i> Hide</a></p>
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
                                            <td>Status</td>
                                            <td>
                                                <span v-if="user.is_active" class="text-success">
                                                    <i class="fas fa-check-circle"></i> Active
                                                </span>
                                                <span v-else class="text-danger">
                                                    <i class="fas fa-times-circle"></i> Suspended
                                                </span>
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>Websites</td>
                                            <td>
                                                {{ user.total_sites }}/{{ user.max_sites }}
                                            </td>
                                        </tr>
                                        <tr>
                                            <td>Databases</td>
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
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    </div>
                    <div class="card mt-3">
                        <div class="card-header bg-primary text-light">
                            Updated User
                        </div>
                        <div class="card-body">
                            <div class="row">
                                <div class="col-md-6">
                                    <div class="form-group">
                                        <label for="max_sites">User Status</label>
                                        <select :class="{'is-invalid': errors.is_active}" class="form-control" v-model="user.is_active">
                                            <option :value="true">Active</option>
                                            <option :value="false">Suspended</option>
                                        </select>
                                        <p class="invalid-feedback" v-if="errors.is_active">{{ errors.is_active[0] }}</p>
                                    </div>
                                    <div class="form-group">
                                        <label for="max_sites">Max. Websites</label>
                                        <input
                                            id="max_sites"
                                            type="text"
                                            class="form-control"
                                            :class="{'is-invalid': errors.max_sites}"
                                            v-model="user.max_sites"
                                            placeholder="Max. websites allowed..."
                                        />
                                        <p class="invalid-feedback" v-if="errors.max_sites">{{ errors.max_sites[0] }}</p>
                                    </div>
                                    <div class="form-group">
                                        <label for="max_sites">Max. Databases</label>
                                        <input
                                            id="max_dbs"
                                            type="text"
                                            class="form-control"
                                            :class="{'is-invalid': errors.max_dbs}"
                                            v-model="user.max_dbs"
                                            placeholder="Max. websites allowed..."
                                        />
                                        <p class="invalid-feedback" v-if="errors.max_dbs">{{ errors.max_dbs[0] }}</p>
                                    </div>
                                    <div class="form-group">
                                        <label for="max_sites">Max. Storage</label>
                                        <input
                                            id="max_dbs"
                                            type="text"
                                            class="form-control"
                                            :class="{'is-invalid': errors.max_storage}"
                                            v-model="user.max_storage"
                                            placeholder="Max. storage allowed in bytes..."
                                        />
                                        <p class="invalid-feedback" v-if="errors.max_storage">{{ errors.max_storage[0] }}</p>
                                    </div>
                                </div>
                            </div>
                        </div>
                        <div class="card-footer">
                            <button @click="saveSettings()" class="btn btn-primary">Save Settings</button>
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
            errors: {},
            new_password: false
        };
    },
    created() {
        this.getUser();
    },
    methods: {
        saveSettings() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('is_active', _this.user.is_active);
            fd.append('max_dbs', _this.user.max_dbs);
            fd.append('max_sites', _this.user.max_sites);
            fd.append('max_storage', _this.user.max_storage);
            axios.patch(`/ssh-users/${_this.user.id}/`, fd).then((res) => {
                _this.$store.commit('setBusy', false);
                _this.getUser();
                toastr.success('User settings have been updated.');
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                toastr.error('User settings cannot be updated.');
            });
        },
        resetPassword() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            _this.new_password = false;
            axios
                .post(`/ssh-users/${_this.$route.params.id}/reset-password/`)
                .then((res) => {
                    _this.$store.commit('setBusy', false);
                    toastr.success('SSH/SFTP password has been updated.');
                    _this.new_password = res.data.new_password;
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
