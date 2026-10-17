# Deploying to AWS EC2

**The instance itself is now provisioned with Terraform — see
`terraform/ec2/README.md` for the full flow (clone-and-build on the
instance, generated SSH key, security group, CD wiring).** This document
covers only the one manual, console-only step that has to happen first:
creating the least-privilege IAM user Terraform/the AWS CLI will act as.

(An earlier, pre-Terraform version of this doc walked through launching the
instance with raw `aws ec2 run-instances` commands and pulling pre-built
GHCR images via `docker-compose.prod.yml`. That path still works if you
prefer it — `docker-compose.prod.yml` is still in the repo — but
`terraform/ec2/` is the supported, repeatable path going forward.)

Steps marked **(console)** must be done in the AWS web console because they
require your AWS account login, not just CLI credentials.

---

## 1. Create a least-privilege IAM user for EC2 management

**(console)** IAM > Users > Create user > name it e.g. `sentinel-ec2-deployer`
> Attach policies directly > skip (we'll use a custom policy) > Create user.

Then create the policy:

1. IAM > Policies > Create policy > JSON tab.
2. Paste the contents of `deploy/iam-policy-ec2-deploy.json` (scoped to
   `ec2:*` actions needed to launch/manage one instance, region-locked to
   `us-east-1` — edit the `aws:RequestedRegion` values first if you use a
   different region).
3. Name it `sentinel-ec2-deploy-policy` and create it.
4. Go back to the `sentinel-ec2-deployer` user > Permissions > Add
   permissions > Attach policy directly > select `sentinel-ec2-deploy-policy`.

Create an access key for this user:

IAM > Users > `sentinel-ec2-deployer` > Security credentials > Create access
key > choose "Command Line Interface (CLI)" > Create.

**Do not paste the Secret Access Key anywhere except your own terminal.**

## 2. Configure the AWS CLI locally

In your own terminal (not through an assistant):

```bash
aws configure
# AWS Access Key ID:     <paste the key from step 1>
# AWS Secret Access Key: <paste the secret from step 1>
# Default region:        us-east-1
# Default output format: json
```

Verify:

```bash
aws sts get-caller-identity
```

## 3. Continue with Terraform

Everything from here — key pair, security group, launching the instance,
cloning the repo, first-time setup, and wiring up CD secrets — is now
handled by `terraform/ec2/`. Go to **`terraform/ec2/README.md`** and
continue from its "Configure variables" step.
