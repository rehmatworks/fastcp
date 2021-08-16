<template>
    <div class="row">
        <databases-table :pagination="true"/>
    </div>
</template>
<script>
import DatabasesTable from './DatabasesTable.vue'
export default {
    components: { DatabasesTable }
}
</script>