<template>
    <div class="row">
        <div class="col-12">
            <div class="card">
                <div class="card-header">
                    <h4 class="float-left mt-1">Create Database</h4>
                    <router-link
                        :to="{ name: 'databases' }"
                        class="btn mt-1 btn-sm float-right btn-warning"
                    >
                        <i class="fas fa-times"></i> Cancel
                    </router-link>
                </div>
                <div class="card-body">
                    <div class="row">
                        <div class="col-md-6">
                            <div class="form-group">
                                <label for="name">Database Name</label>
                                <input
                                    id="name"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.name}"
                                    v-model="name"
                                    placeholder="Database name..."
                                />
                                <p class="invalid-feedback" v-if="errors.name">{{ errors.name[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="username">Database Username</label>
                                <input
                                    id="username"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.username}"
                                    v-model="username"
                                    placeholder="Database username..."
                                />
                                <p class="invalid-feedback" v-if="errors.username">{{ errors.username[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="password">User Password</label>
                                <input
                                    id="password"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.password}"
                                    v-model="password"
                                    placeholder="Database user password..."
                                />
                                <p class="invalid-feedback" v-if="errors.password">{{ errors.password[0] }}</p>
                            </div>
                        </div>
                    </div>
                </div>
                <div class="card-footer">
                    <button @click="createDatabase()" class="btn btn-primary">Create</button>
                </div>
            </div>
        </div>
    </div>
</template>
<script>
export default {
    data() {
        return {
            name: '',
            username: '',
            password: '',
            errors: {}
        }
    },
    created() {
        this.password = this.genRandPassword();
    },
    methods: {
        createDatabase() {
            let _this = this;
            _this.errors = {};
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('name', _this.name);
            fd.append('username', _this.username);
            fd.append('password', _this.password);
            axios.post('/databases/', fd).then((res) => {
                _this.$store.commit('setBusy', false);
                toastr.success('Databases has been created successfully.');
                _this.$router.push({name: 'databases'});
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                _this.errors = err.response.data;
            });
        }
    }
};
</script>
