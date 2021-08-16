<template>
    <div class="row">
        <users-table :pagination="true"/>
    </div>
</template>
<script>
import UsersTable from './UsersTable.vue'
export default {
    components: { UsersTable }
}
</script>