<template>
    <div class="row">
        <div class="col-12">
            <div class="card">
                <div class="card-header">
                    <h4 class="float-left mt-1">Deploy Website</h4>
                    <router-link
                        :to="{ name: 'websites' }"
                        class="btn mt-1 btn-sm float-right btn-warning"
                    >
                        <i class="fas fa-times"></i> Cancel
                    </router-link>
                </div>
                <div class="card-body">
                    <div class="row">
                        <div class="col-md-6">
                            <div class="form-group">
                                <label for="label">Website Label</label>
                                <input
                                    id="label"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.label}"
                                    v-model="label"
                                    placeholder="A unique label to identify your website..."
                                />
                                <p class="invalid-feedback" v-if="errors.label">{{ errors.label[0] }}</p>
                            </div>
                            <div class="form-group">
                                <label for="php">PHP Version</label>
                                <select :class="{'is-invalid': errors.php}" id="php" v-model="php" class="form-control">
                                    <option v-for="php in php_versions" :key="php" :value="php">PHP {{ php }}</option>
                                </select>
                                <p class="invalid-feedback" v-if="errors.php">{{ errors.php[0] }}</p>
                            </div>
                            <div v-if="$store.state.user && $store.state.user.is_root" class="form-group">
                                <label for="user">
                                    SSH User
                                    <small>
                                        <a @click="create=!create" href="javascript:void(0)" class="text-primary text-decoration-none">
                                            <span v-if="create">Cancel</span>
                                            <span v-else>
                                                <i class="fas fa-plus"></i> Create
                                            </span>
                                        </a>
                                    </small>
                                </label>
                                <usercreate-component v-if="create"/>
                                <v-select v-else :options="users" @search="getUsers" v-model="ssh_user"></v-select>
                                <p class="text-danger" v-if="!create && errors.username">{{ errors.username }}</p>
                            </div>
                            <div class="form-group">
                                <label for="website_type">Website Type</label>
                                <select class="form-control" v-model="website_type">
                                    <option value="blank">Blank PHP Website</option>
                                    <option value="wordpress">WordPress Website</option>
                                </select>
                            </div>
                            <div class="form-group">
                                <label for="domains">Domains</label>
                                <input
                                    id="domains"
                                    type="text"
                                    class="form-control"
                                    :class="{'is-invalid': errors.domains}"
                                    v-model="domains"
                                    placeholder="Comma-separated domains list, i.e. example.com, www.example.com"
                                />
                                <p class="invalid-feedback" v-if="errors.domains">{{ errors.domains[0] }}</p>
                            </div>
                        </div>
                    </div>
                </div>
                <div class="card-footer">
                    <button :disabled="create" @click="createWebsite()" class="btn btn-primary">Deploy Now</button>
                </div>
            </div>
        </div>
    </div>
</template>
<script>
export default {
    data() {
        return {
            label: '',
            php: '',
            php_versions: [],
            domains: '',
            errors: {},
            users: [],
            ssh_user: '',
            create: false,
            website_type: 'blank',
            wpuser: 'admin',
            email: '',
            password: ''
        }
    },
    created() {
        this.getPhpVersions();
        if(this.$store.state.user && this.$store.state.user.is_root) {
            this.getUsers();
        }
        this.EventBus.$on('userCreated', this.handleUserCreated);
    },
    methods: {
        handleUserCreated(username) {
            this.create = false;
            this.ssh_user = username;
            this.getUsers();
        },
        getUsers(search='', loading=false) {
            let _this = this;
            _this.errors = {};
            if(loading) {
                loading(true);
            }
            _this.$store.commit('setBusy', true);
            axios.get(`/ssh-users/?q=${search}`).then((res) => {
                _this.$store.commit('setBusy', false);
                var users = [];
                for(var i = 0; i < res.data.results.length; i++) {
                    users.push(res.data.results[i].username);
                }
                if(loading) {
                    loading(false);
                }
                _this.users = users;
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                toastr.error('SSH users list cannot be obtained.');
                if(loading) {
                    loading(false);
                }
            });
        },
        getPhpVersions() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            axios.get('/websites/php-versions/').then((res) => {
                _this.php_versions = res.data.php_versions;
                _this.php = _this.php_versions[0];
                _this.$store.commit('setBusy', false);
            }).catch((err) => {
                toastr.error('Supported PHP versions list cannot be retrieved.');
                _this.$store.commit('setBusy', false);
            });
        },
        createWebsite() {
            let _this = this;
            _this.errors = {};
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('label', _this.label);
            fd.append('php', _this.php);
            fd.append('ssh_user', _this.ssh_user);
            fd.append('domains', _this.domains);
            fd.append('website_type', _this.website_type);
            axios.post('/websites/', fd).then((res) => {
                _this.$store.commit('setBusy', false);
                toastr.success('Website has been added successfully.');
                _this.$router.push({name: 'websites'});
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                _this.errors = err.response.data;
            });
        }
    },
    watch: {
        create(newval, oldval) {
            if(newval) {
                this.user_pass = this.genRandPassword();
                this.ssh_user = '';
            }
            this.errors = {};
        }
    }
};
</script>
