<template>
    <div class="input-group">
        <input
            :class="{ 'is-invalid': errors.username }"
            v-model="ssh_user"
            placeholder="Username"
            type="text"
            class="form-control"
        />
        <input
            type="text"
            :class="{ 'is-invalid': errors.password }"
            placeholder="Password"
            class="form-control"
            v-model="user_pass"
        />
        <div class="input-group-append">
            <button
                @click="createUser()"
                :class="{
                    'btn-outline-secondary': !errors.username,
                    'btn-outline-danger': errors.username,
                }"
                class="btn"
                type="button"
            >
                Creat User
            </button>
        </div>
        <p class="invalid-feedback" v-if="errors.username">{{ errors.username[0] }}</p>
    </div>
</template>
<script>
export default {
    data() {
        return {
            ssh_user: '',
            user_pass: '',
            errors: {}
        }
    },
    created() {
        this.user_pass = this.genRandPassword();
    },
    methods: {
        createUser() {
            let _this = this;
            _this.$store.commit('setBusy', true);
            let fd = new FormData();
            fd.append('username', _this.ssh_user);
            fd.append('password', _this.user_pass);
            axios.post('/ssh-users/', fd).then((res) => {
                toastr.success('SSH user has been created successfully.');
                _this.$store.commit('setBusy', false);
                _this.EventBus.$emit('userCreated', res.data.username);
            }).catch((err) => {
                _this.$store.commit('setBusy', false);
                _this.errors = err.response.data;
             });
        },
    }
}
</script>
