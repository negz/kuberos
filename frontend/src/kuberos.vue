<template>
  <div id="kuberos">
    <el-container fluid>
        <el-alert v-if="error" title="Authentication failed" type="error" :description="`${error.response.status} ${error.response.statusText}: ${error.response.data}`" show-icon closable="false"></el-alert>
        <el-alert v-else title="Successfully Authenticated" type="success" center show-icon>
  </el-alert>
      <el-header>
        <h1>Kuberos</h1>
        <el-menu :default-active="activeIndex" class="el-menu-demo" mode="horizontal" @select="handleSelect">
          <el-menu-item index="1"><a href="#intro">Getting Started</a></el-menu-item>
          <el-menu-item index="2"><a href="#kubectl">Running Kubectl</a></el-menu-item>
          <el-menu-item index="3"><a href="#manual">Advanced</a></el-menu-item>
        </el-menu>
      </el-header>
      <el-main>
        <el-card class="box-card" id="intro">
        <el-row :gutter="10">
          <el-col :xs="24">
            <h2>Getting Started</h2>
            <hr class="mb2">
            <a>Save the file below as  <code>~/.kube/config</code> to enable OIDC based <code>kubectl</code> authentication.</a>
          </el-col>
        </el-row>
        <el-row :gutter="10" class="mt2">
          <el-col :xs="24">
           <el-button type="primary" icon="el-icon-download" @click="open">Download Config File</el-button>
          </el-col>
        </el-row>
        </el-card>
        <el-card class="box-card mt2" id="kubectl">
        <el-row :gutter="10">
          <el-col :xs="24">
            <h2>Running kubectl</h2>
            <hr class="mb2">
           <a>Once you've saved the above <code>~/.kube/config</code> file you should be able to run <code>kubectl</code></a>
           <pre v-highlightjs>
             <code class="console">
# These are examples. Your context and cluster names will likely differ.

$ kubectl config get-contexts
CURRENT   NAME          CLUSTER                    AUTHINFO   NAMESPACE
          experimental  experimental.example.org   kuberos
          prod          prod.example.org           kuberos

$ kubectl --context experimental get namespaces
NAME          STATUS    AGE
default       Active    83d
experimental  Active    15d

$ kubectl --context experimental -n experimental get pods
NAME                                       READY     STATUS             RESTARTS   AGE
kuberos-4074452424-06m0b                   1/1       Running            1          6d
              </code>
            </pre>
          </el-col>
        </el-row>
        </el-card>
        <el-card class="box-card mt2" id="manual">
        <el-row :gutter="10">
          <el-col :xs="24">
            <h2>Authenticate Manually</h2>
            <hr class="mb2">
           <a>If you want to maintain your existing <code>~/.kube/config</code> file you can run the following to add your user:</a>
           <pre v-highlightjs="snippetSetCreds()"><code class="bash"></code></pre>
          </el-col>
        </el-row>
        </el-card>
      </el-main>
      <el-footer>
      </el-footer>
    </el-container>
  </div>
</template>
<script>
export default {
  name: "kuberos",
  metaInfo: {
    title: "Kubernetes Authentication",
    htmlAttrs: {
      lang: "en"
    }
  },
  data: function() {
    return {
      error: null,
      activeIndex: "1",
      kubecfg: {}
    };
  },
  methods: {
    handleSelect(key, keyPath) {
      console.log(key, keyPath);
    },
    open() {
      window.location.href = this.templateURL();
      this.$message({
        message: "Download started!",
        type: "success"
      });
    },
    templateURL: function() {
      return "kubecfg.yaml?" + $.param(this.kubecfg);
    },
    snippetSetCreds: function() {
      return (
        "# Add your user to kubectl\n" +
        'kubectl config set-credentials "' +
        this.kubecfg.email +
        '" \\\n' +
        "  --auth-provider=oidc \\\n" +
        '  --auth-provider-arg=client-id="' +
        this.kubecfg.clientID +
        '" \\\n' +
        '  --auth-provider-arg=client-secret="' +
        this.kubecfg.clientSecret +
        '" \\\n' +
        '  --auth-provider-arg=id-token="' +
        this.kubecfg.idToken +
        '" \\\n' +
        '  --auth-provider-arg=refresh-token="' +
        this.kubecfg.refreshToken +
        '" \\\n' +
        '  --auth-provider-arg=idp-issuer-url="' +
        this.kubecfg.issuer +
        '"\n\n' +
        "# Associate your user with an existing cluster\n" +
        "export CLUSTER=coolcluster\n" +
        "export CONTEXT=coolcontext\n" +
        'kubectl config set-context ${CONTEXT} --cluster ${CLUSTER} --user="' +
        this.kubecfg.email +
        '"'
      );
    }
  },
  created: function() {
    var q = decodeURI(location.search.substr(1))
      .replace(/"/g, '\\"')
      .replace(/&/g, '","')
      .replace(/=/g, '":"');
    var query = "";
    if (q != "") {
      query = JSON.parse('{"' + q + '"}');
    }
    var url = "kubecfg?" + $.param(query);

    var _this = this;
    this.axios
      .get(url)
      .then(function(response) {
        _this.kubecfg = response.data;
        if (_this.kubecfg.email == "") {
          _this.kubecfg.email = "kuberos";
        }
      })
      .catch(function(error) {
        _this.error = error;
      });
  }
};
</script>
