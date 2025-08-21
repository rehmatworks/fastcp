<template>
    <div class="row">
        <div class="col-12">
            <div class="card">
                <div class="card-header">
                    <h4 class="float-left mt-1">Create User</h4>
                    <router-link
                        :to="{ name: 'users' }"
                        class="btn mt-1 btn-sm float-right btn-warning"
                    >
                        <i class="fas fa-times"></i> Cancel
                    </router-link>
                </div>
                <div class="card-body">
                    <div class="row">
                        <div class="col-md-6">
                            <div class="form-group">
                                <label for="username">Username</label>
                                <input
                                    id="username"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.username}"
                                    v-model="username"
                                    placeholder="SSH username"
                                />
                                <p class="invalid-feedback" v-if="errors.username">{{ errors.username[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="password">Password</label>
                                <input
                                    id="password"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.password}"
                                    v-model="password"
                                    placeholder="SSH & control panel login password"
                                />
                                <p class="invalid-feedback" v-if="errors.password">{{ errors.password[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="max_sites">Max. Websites</label>
                                <input
                                    id="max_sites"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.max_sites}"
                                    v-model="max_sites"
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
                                    v-model="max_dbs"
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
                                    v-model="max_storage"
                                    placeholder="Max. storage allowed in bytes..."
                                />
                                <p class="invalid-feedback" v-if="errors.max_storage">{{ errors.max_storage[0] }}</p>
                            </div>
                        </div>
                    </div>
                </div>
                <div class="card-footer">
                    <button @click="createUser()" class="btn btn-primary">Create User</button>
                </div>
            </div>
        </div>
    </div>
</template>
<script>
export default {
    data() {
        return {
            errors: {},
            username: '',
            max_sites: 10,
            max_dbs: 10,
            max_storage: '',
            is_active: true,
            password: ''
        }
    },
    created() {
        this.password = this.genRandPassword();
    },
    methods: {
        createUser() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('username', _this.username);
            fd.append('password', _this.password);
            fd.append('max_sites', _this.max_sites);
            fd.append('max_dbs', _this.max_dbs);
            fd.append('max_storage', _this.max_storage);
            fd.append('is_active', _this.is_active);
            axios.post('/ssh-users/', fd).then((res) => {
                toastr.success('SSH user has been created successfully.');
                _this.$store.commit('setBusy', false);
                _this.$router.push({name: 'users'});
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                if(err.response) {
                    _this.errors = err.response.data;
                }
                toastr.error('SSH user cannot be created.');
             });
        }
    }
};
</script>
